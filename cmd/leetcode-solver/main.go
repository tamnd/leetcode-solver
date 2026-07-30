package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tamnd/leetcode-solver/agentbench"
	"github.com/tamnd/leetcode-solver/artifact"
	"github.com/tamnd/leetcode-solver/codex"
	"github.com/tamnd/leetcode-solver/completedataset"
	"github.com/tamnd/leetcode-solver/config"
	benchmark "github.com/tamnd/leetcode-solver/eval"
	"github.com/tamnd/leetcode-solver/evaldataset"
	"github.com/tamnd/leetcode-solver/judge"
	"github.com/tamnd/leetcode-solver/llm"
	"github.com/tamnd/leetcode-solver/offline"
	"github.com/tamnd/leetcode-solver/problem"
	"github.com/tamnd/leetcode-solver/solver"
	"github.com/tamnd/leetcode-solver/source"
)

var version = "dev"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "solve":
		return solve(ctx, args[1:])
	case "batch":
		return batch(ctx, args[1:])
	case "catalog":
		return catalog(ctx, args[1:])
	case "sync":
		return syncDataset(ctx, args[1:])
	case "convert":
		return convertDataset(ctx, args[1:])
	case "complete-sync":
		return syncCompleteDataset(ctx, args[1:])
	case "reference":
		return showReference(ctx, args[1:])
	case "eval-sync":
		return syncEvalDataset(ctx, args[1:])
	case "coverage":
		return coverage(ctx, args[1:])
	case "eval":
		return evaluate(args[1:])
	case "verify":
		return verifyOffline(ctx, args[1:])
	case "agent-solve":
		return agentSolve(ctx, args[1:])
	case "agent-bench":
		return agentBench(ctx, args[1:])
	case "bridge":
		return runBridge(ctx, args[1:])
	case "version":
		fmt.Println(version)
		return nil
	default:
		return usage()
	}
}
func usage() error {
	fmt.Fprintln(os.Stderr, "usage: leetcode-solver <sync|complete-sync|convert|reference|eval-sync|coverage|solve|batch|catalog|verify|eval|bridge|agent-solve|agent-bench|version> [options]")
	return errors.New("command is required")
}

// runBridge fronts a ChatGPT subscription with an OpenAI-compatible endpoint,
// so any of this repository's commands can run on the Codex CLI's credential
// by pointing LEETCODE_SOLVER_BASE_URL at the printed address. `--provider
// codex` reaches the same client in process; the bridge exists for the tools
// that only speak HTTP, including the Codex CLI itself through /v1/responses.
func runBridge(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bridge", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "listen address; a non-loopback address requires --api-key")
	port := fs.Int("port", 8790, "listen port")
	apiKey := fs.String("api-key", "", "key clients must present as a bearer token")
	authPath := fs.String("auth", "", "Codex credential file; defaults to ~/.codex/auth.json")
	model := fs.String("model", codex.DefaultModel, "model used when a request does not name one")
	effort := fs.String("effort", "", "reasoning effort applied when a request does not set one")
	timeout := fs.Duration("timeout", 30*time.Minute, "upstream request timeout")
	retries := fs.Int("retries", 2, "retries for transient upstream failures")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("bridge takes no positional arguments")
	}
	logf := func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
	auth := &codex.Auth{Path: *authPath, Logf: logf}
	// Read the credential before binding the port. Failing here with a clear
	// message beats a listener that answers every request with the same 502.
	token, err := auth.Token(ctx)
	if err != nil {
		return err
	}
	bridge := &codex.Bridge{Client: &codex.Client{Auth: auth, HTTPClient: &http.Client{Timeout: *timeout}, MaxRetries: *retries, Effort: *effort}, APIKey: *apiKey, Model: *model, Logf: logf}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	expiry := "unknown"
	if !token.ExpiresAt.IsZero() {
		expiry = token.ExpiresAt.Local().Format(time.RFC3339)
	}
	return bridge.Serve(ctx, *host, *port, func(address string) {
		plan := token.PlanType
		if plan == "" {
			plan = "unknown"
		}
		fmt.Printf("leetcode-solver bridge on http://%s, plan %s, token expires %s, default model %s\n", address, plan, expiry, *model)
		if *apiKey == "" {
			fmt.Println("no --api-key set, so any local process can spend this plan's quota")
		}
	})
}

func agentBench(ctx context.Context, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("agent-bench", flag.ContinueOnError)
	repo := fs.String("repo", wd, "leetcode-solver repository")
	labRepo := fs.String("lab-repo", filepath.Join(filepath.Dir(wd), "tomo-labs"), "tomo-labs repository containing the pinned commit")
	workspace := fs.String("workspace", agentbench.DefaultWorkspace(), "recreated pinned lab workspace")
	cache := fs.String("cache", filepath.Join(wd, ".cache", "agentbench"), "verified dataset cache")
	data := fs.String("data", filepath.Join(wd, ".cache", "agentbench-runs"), "raw isolated run data")
	offlineOnly := fs.Bool("offline", false, "require cached dataset and perform no dataset download")
	prepareOnly := fs.Bool("prepare-only", false, "prepare tasks and adapter without running agents")
	skipBuild := fs.Bool("skip-build", false, "reuse prebuilt lab images")
	tools := fs.String("tools", "", "comma-separated tool subset")
	providers := fs.String("providers", "", "comma-separated deepseek,luna,mini, or zen/<model> routes")
	scenarios := fs.String("scenarios", "", "comma-separated scenario subset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	split := func(s string) []string {
		var out []string
		for _, v := range strings.Split(s, ",") {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return agentbench.Run(ctx, agentbench.RunOptions{Repository: *repo, LabRepository: *labRepo, Workspace: *workspace, Cache: *cache, Data: *data, Offline: *offlineOnly, PrepareOnly: *prepareOnly, SkipBuild: *skipBuild, Tools: split(*tools), Providers: split(*providers), Scenarios: split(*scenarios), Progress: func(s string) { fmt.Fprintln(os.Stderr, "[agent-bench]", s) }})
}

// agentSolve is the isolated benchmark entrypoint. It sees the same visible
// task as every coding agent and never sees the sibling hidden-test oracle.
func agentSolve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-solve", flag.ContinueOnError)
	promptPath := fs.String("prompt", "/scenario/prompt.txt", "visible task prompt")
	outputPath := fs.String("output", "/work/solution.py", "solution file to replace")
	baseURL := fs.String("base-url", os.Getenv("LAB_BASE_URL"), "traced Responses endpoint")
	model := fs.String("model", os.Getenv("LAB_MODEL"), "model under test")
	apiKey := fs.String("api-key", os.Getenv("OPENCODE_API_KEY"), "proxy credential")
	if err := fs.Parse(args); err != nil {
		return err
	}
	task, err := os.ReadFile(*promptPath)
	if err != nil {
		return err
	}
	stub, err := os.ReadFile(*outputPath)
	if err != nil {
		return err
	}
	instructions := "You are the leetcode-solver coding engine. Return a correct Python solution after carefully deriving edge cases and complexity. Reply with exactly one fenced python code block and no other fenced blocks. Do not use the network and do not attempt to access hidden tests."
	input := string(task) + "\n\nCurrent solution.py:\n```python\n" + string(stub) + "\n```"
	response, err := (&llm.Client{BaseURL: *baseURL, APIKey: *apiKey}).ChatComplete(ctx, *model, instructions, input)
	if err != nil {
		return err
	}
	code, err := fencedPython(response)
	if err != nil {
		return err
	}
	return os.WriteFile(*outputPath, []byte(code), 0o644)
}

func fencedPython(s string) (string, error) {
	start := strings.Index(s, "```")
	if start < 0 {
		return "", errors.New("model returned no fenced solution")
	}
	rest := s[start+3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		return "", errors.New("malformed solution fence")
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return "", errors.New("unterminated solution fence")
	}
	code := strings.TrimSpace(rest[:end])
	if code == "" {
		return "", errors.New("empty solution")
	}
	return code + "\n", nil
}

func flags(name string) (*flag.FlagSet, *config.Config, *bool) {
	cfg := config.Load()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	force := fs.Bool("force", false, "ignore an accepted cached result")
	fs.StringVar(&cfg.Provider, "provider", cfg.Provider, "api for a Responses endpoint, chat for a chat-completions gateway, or codex for the local ChatGPT subscription credential")
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "OpenAI-compatible API base URL")
	fs.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "API key (prefer environment variable)")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "model name")
	fs.StringVar(&cfg.Language, "language", cfg.Language, "LeetCode language slug")
	fs.StringVar(&cfg.LeetCodeURL, "leetcode-url", cfg.LeetCodeURL, "LeetCode base URL")
	fs.StringVar(&cfg.Output, "output", cfg.Output, "artifact directory")
	fs.StringVar(&cfg.Database, "database", cfg.Database, "local LeetCode SQLite or DuckDB path")
	fs.StringVar(&cfg.EvalRoot, "eval-root", cfg.EvalRoot, "offline evaluation bundle root")
	fs.StringVar(&cfg.ContainerRuntime, "container-runtime", cfg.ContainerRuntime, "docker, podman, or auto")
	fs.IntVar(&cfg.Candidates, "candidates", cfg.Candidates, "independent candidates, 1-5")
	fs.IntVar(&cfg.MaxRepairs, "max-repairs", cfg.MaxRepairs, "execution-guided code repair attempts")
	fs.IntVar(&cfg.MaxCorrections, "max-corrections", cfg.MaxCorrections, "article rewrites after a quality judge rejects it")
	return fs, &cfg, force
}

// completer is the model endpoint a run talks to. The codex provider spends a
// ChatGPT subscription through the Codex CLI's own credential and needs no key
// and no listening port; chat is for the many gateways that implement only
// chat completions; api is the Responses endpoint, which the bridge also is.
func completer(cfg config.Config) llm.Completer {
	switch cfg.Provider {
	case "codex":
		logf := func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
		return &codex.Client{Auth: &codex.Auth{Logf: logf}, MaxRetries: 2}
	case "chat":
		return &llm.ChatClient{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, MaxRetries: 2}
	}
	return &llm.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, MaxRetries: 2}
}

// resolveProvider refuses an unrecognized provider rather than quietly falling
// back to the API path, where a subscription-only setup would fail later with a
// missing-credential error that names the wrong cause.
//
// It also repairs the model when --provider codex is chosen on the command line
// after the environment already defaulted the model for the API: the two are
// separate slug namespaces, so carrying an API default into a subscription
// request fails on the first call for a reason that reads like an outage.
func resolveProvider(fs *flag.FlagSet, cfg *config.Config) error {
	switch cfg.Provider {
	case "api", "chat":
		return nil
	case "codex":
	default:
		return fmt.Errorf("unknown provider %q: use api, chat, or codex", cfg.Provider)
	}
	chosen := os.Getenv("LEETCODE_SOLVER_MODEL") != ""
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "model" {
			chosen = true
		}
	})
	if !chosen {
		cfg.Model = codex.DefaultModel
	}
	return nil
}
func engine(cfg config.Config) *solver.Engine {
	return &solver.Engine{Client: completer(cfg), Judge: &judge.LeetCode{BaseURL: cfg.LeetCodeURL, Session: cfg.Session, CSRFToken: cfg.CSRFToken}, Offline: &offline.Runner{Root: cfg.EvalRoot, DockerBinary: cfg.ContainerRuntime}, Store: artifact.Store{Root: cfg.Output}, Progress: func(message string) { fmt.Fprintln(os.Stderr, message) }}
}

func syncDataset(ctx context.Context, args []string) error {
	cfg := config.Load()
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.StringVar(&cfg.Database, "database", cfg.Database, "SQLite dataset path")
	fs.StringVar(&cfg.LeetCodeURL, "leetcode-url", cfg.LeetCodeURL, "LeetCode base URL")
	delay := fs.Duration("delay", 50*time.Millisecond, "delay between public requests")
	if err := fs.Parse(args); err != nil {
		return err
	}
	database, err := source.OpenSQLite(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	fetcher := source.Fetcher{Client: source.New(cfg.LeetCodeURL), Delay: *delay, Progress: func(done, total int, slug string) { fmt.Fprintf(os.Stderr, "\r%d/%d %s", done, total, slug) }}
	err = fetcher.Sync(ctx, database)
	fmt.Fprintln(os.Stderr)
	return err
}

func syncEvalDataset(ctx context.Context, args []string) error {
	cfg := config.Load()
	fs := flag.NewFlagSet("eval-sync", flag.ContinueOnError)
	revision := fs.String("revision", "a9eca795817a0a21132070e2dc2e87445da4f089", "pinned Hugging Face dataset revision")
	versionName := fs.String("version", "v0.3.1", "LeetCodeDataset version")
	image := fs.String("python-image", "python:3.13-alpine@sha256:399babc8b49529dabfd9c922f2b5eea81d611e4512e3ed250d75bd2e7683f4b0", "preloaded digest-pinned Python image")
	goImage := fs.String("go-image", "golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2", "preloaded digest-pinned Go image")
	gapReport := fs.String("go-gaps", "", "write the problems that received no Go bundle, and why, to this JSON path")
	fs.StringVar(&cfg.EvalRoot, "eval-root", cfg.EvalRoot, "offline evaluation bundle root")
	fs.StringVar(&cfg.Database, "database", cfg.Database, "problem repository supplying Go starter signatures")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The dataset rows carry only Python. A Go bundle needs the problem's Go starter
	// signature, which lives in the problem repository, so the lookup is injected and
	// the importer stays a pure function of its inputs.
	signature := func(string) (string, bool) { return "", false }
	if repository, openErr := source.Open(cfg.Database); openErr == nil {
		defer func() { _ = repository.Close() }()
		signature = func(slug string) (string, bool) {
			p, problemErr := repository.Problem(ctx, slug)
			if problemErr != nil {
				return "", false
			}
			snippet, ok := p.Snippet("golang")
			return snippet.Code, ok
		}
	} else {
		fmt.Fprintf(os.Stderr, "no problem repository at %s, so no Go bundles will be generated: %v\n", cfg.Database, openErr)
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	totalRows, totalBundles, totalTests, totalGoBundles, totalGoTests := 0, 0, 0, 0, 0
	var gaps []evaldataset.GoGap
	for _, split := range []string{"train", "test"} {
		url := fmt.Sprintf("https://huggingface.co/datasets/newfacade/LeetCodeDataset/resolve/%s/LeetCodeDataset-%s-%s.jsonl", *revision, *versionName, split)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "leetcode-solver/0.1")
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			return fmt.Errorf("download %s: %s: %s", split, response.Status, body)
		}
		report, importErr := evaldataset.Import(ctx, response.Body, evaldataset.Options{Root: cfg.EvalRoot, PythonImage: *image, GoImage: *goImage, Revision: "hf:" + *revision + ":" + *versionName, GoSignature: signature})
		_ = response.Body.Close()
		if importErr != nil {
			return importErr
		}
		totalRows += report.Rows
		totalBundles += report.Bundles
		totalTests += report.Tests
		totalGoBundles += report.GoBundles
		totalGoTests += report.GoTests
		gaps = append(gaps, report.GoGaps...)
		fmt.Fprintf(os.Stderr, "imported %s: %d rows, %d Python bundles, %d Python tests, %d missing, %d Go bundles, %d Go tests, %d Go gaps\n",
			split, report.Rows, report.Bundles, report.Tests, len(report.MissingTests), report.GoBundles, report.GoTests, len(report.GoGaps))
	}
	if *gapReport != "" {
		data, err := json.MarshalIndent(gaps, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*gapReport, append(data, '\n'), 0o600); err != nil {
			return err
		}
	}
	fmt.Printf("read %d rows; imported %d Python bundles with %d offline tests and %d Go bundles with %d offline tests; %d problems have no Go bundle\n",
		totalRows, totalBundles, totalTests, totalGoBundles, totalGoTests, len(gaps))
	return nil
}

func syncCompleteDataset(ctx context.Context, args []string) error {
	cfg := config.Load()
	fs := flag.NewFlagSet("complete-sync", flag.ContinueOnError)
	fs.StringVar(&cfg.Database, "database", cfg.Database, "destination SQLite problem and reference database")
	fs.StringVar(&cfg.CompleteCache, "cache", cfg.CompleteCache, "raw pinned dataset cache")
	revision := fs.String("revision", completedataset.DefaultRevision, "pinned Hugging Face revision")
	offlineOnly := fs.Bool("offline", false, "prohibit downloads and require a verified cache")
	if err := fs.Parse(args); err != nil {
		return err
	}
	database, err := source.OpenSQLite(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	report, err := completedataset.Sync(ctx, database, completedataset.Options{Revision: *revision, CacheDir: cfg.CompleteCache, Offline: *offlineOnly, Progress: func(message string) { fmt.Fprintln(os.Stderr, message) }})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}

func showReference(ctx context.Context, args []string) error {
	cfg := config.Load()
	fs := flag.NewFlagSet("reference", flag.ContinueOnError)
	fs.StringVar(&cfg.Database, "database", cfg.Database, "SQLite reference database")
	sourceName := fs.String("source", completedataset.Source, "reference dataset source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("reference requires a problem slug or ID")
	}
	database, err := source.OpenSQLite(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	value, err := database.ReferenceData(ctx, *sourceName, fs.Arg(0))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(value)
}

func convertDataset(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fromPath := fs.String("from", "", "source .sqlite or .duckdb path")
	toPath := fs.String("to", "", "destination .sqlite path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *fromPath == "" || *toPath == "" {
		return errors.New("convert requires --from and --to")
	}
	from, err := source.Open(*fromPath)
	if err != nil {
		return err
	}
	defer func() { _ = from.Close() }()
	to, err := source.OpenSQLite(*toPath)
	if err != nil {
		return err
	}
	defer func() { _ = to.Close() }()
	err = source.Convert(ctx, from, to, func(done, total int, slug string) { fmt.Fprintf(os.Stderr, "\r%d/%d %s", done, total, slug) })
	fmt.Fprintln(os.Stderr)
	return err
}

type coverageReport struct {
	CatalogProblems         int      `json:"catalog_problems"`
	RequiredImplementations int      `json:"required_implementations"`
	CoveredImplementations  int      `json:"covered_implementations"`
	Tests                   int      `json:"tests"`
	Missing                 []string `json:"missing"`
}

func coverage(ctx context.Context, args []string) error {
	fs, cfg, _ := flags("coverage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	repository, err := source.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = repository.Close() }()
	items, err := repository.Catalog(ctx)
	if err != nil {
		return err
	}
	report := coverageReport{CatalogProblems: len(items)}
	for _, item := range items {
		p, problemErr := repository.Problem(ctx, item.Slug)
		if problemErr != nil {
			report.Missing = append(report.Missing, item.Slug+":source")
			continue
		}
		// Coverage counts what a complete answer owes, not what happens to have a
		// bundle: filtering by bundle presence here would report full coverage of
		// whatever was already generated.
		languages := requiredLanguages(p)
		if cfg.Language != "auto" {
			if _, ok := p.Snippet(cfg.Language); !ok {
				report.Missing = append(report.Missing, item.Slug+":language")
				continue
			}
			languages = []string{cfg.Language}
		}
		if len(languages) == 0 {
			report.Missing = append(report.Missing, item.Slug+":language")
			continue
		}
		for _, language := range languages {
			report.RequiredImplementations++
			manifest, manifestErr := offline.ReadManifest(cfg.EvalRoot, item.Slug, language)
			if manifestErr != nil {
				report.Missing = append(report.Missing, item.Slug+":"+language)
				continue
			}
			report.CoveredImplementations++
			report.Tests += manifest.TestCount
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return err
	}
	if len(report.Missing) > 0 {
		return fmt.Errorf("offline evaluation coverage is incomplete: %d implementations missing", len(report.Missing))
	}
	return nil
}

func solve(ctx context.Context, args []string) error {
	fs, cfg, force := flags("solve")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("solve requires a problem slug")
	}
	if err := resolveProvider(fs, cfg); err != nil {
		return err
	}
	repository, err := source.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = repository.Close() }()
	p, err := repository.Problem(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	languages, gaps, err := chooseLanguages(p, cfg.Language, cfg.EvalRoot)
	if err != nil {
		return err
	}
	for _, language := range gaps {
		fmt.Fprintf(os.Stderr, "skipping %s: no offline eval bundle under %s, so a %s article could not be proved correct\n", language, cfg.EvalRoot, language)
	}
	var results []artifact.Result
	for _, language := range languages {
		result, solveErr := engine(*cfg).Solve(ctx, p, solver.Options{Model: cfg.Model, Language: language, Candidates: cfg.Candidates, MaxRepairs: cfg.MaxRepairs, MaxCorrections: cfg.MaxCorrections, Force: *force, Submit: cfg.Session != "" && cfg.CSRFToken != ""})
		if solveErr != nil {
			return fmt.Errorf("%s: %w", language, solveErr)
		}
		// The executed assertion count is the honest number: it is what the harness
		// reported running, not what the bundle declared it would run.
		fmt.Printf("accepted offline: %s (%d), %s, %d assertions executed of %d declared, article verified by %d judges",
			p.Title, pID(p.FrontendID), language, result.Offline.AssertionsRun, result.Offline.TestCount, len(result.Quality))
		if result.SubmissionID > 0 {
			fmt.Printf(", submission %d", result.SubmissionID)
		}
		fmt.Println()
		fmt.Println(filepath.Join(cfg.Output, p.Slug, language+".md"))
		results = append(results, result)
	}
	if len(results) == 2 {
		path, combineErr := (artifact.Store{Root: cfg.Output}).SaveCombined(p.Slug, results)
		if combineErr != nil {
			return combineErr
		}
		fmt.Println(path)
	}
	return nil
}
func pID(value string) int { n, _ := strconv.Atoi(value); return n }

func catalog(ctx context.Context, args []string) error {
	fs, cfg, _ := flags("catalog")
	output := fs.String("json", "", "write catalog JSON to this path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	repository, err := source.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = repository.Close() }()
	items, err := repository.Catalog(ctx)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if *output != "" {
		return os.WriteFile(*output, data, 0o644)
	}
	_, err = os.Stdout.Write(data)
	return err
}

func batch(ctx context.Context, args []string) error {
	fs, cfg, force := flags("batch")
	parallel := fs.Int("parallel", 1, "concurrent problems")
	includePaid := fs.Bool("include-paid", false, "include paid-only problems")
	limit := fs.Int("limit", 0, "maximum number of catalog problems, zero means all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *parallel < 1 || *parallel > 16 {
		return errors.New("parallel must be between 1 and 16")
	}
	if err := resolveProvider(fs, cfg); err != nil {
		return err
	}
	repository, err := source.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = repository.Close() }()
	items, err := repository.Catalog(ctx)
	if err != nil {
		return err
	}
	jobs := make(chan string)
	errs := make(chan error, len(items))
	var wg sync.WaitGroup
	for range *parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for slug := range jobs {
				p, err := repository.Problem(ctx, slug)
				if err == nil {
					languages, gaps, languageErr := chooseLanguages(p, cfg.Language, cfg.EvalRoot)
					for _, language := range gaps {
						fmt.Fprintf(os.Stderr, "skipping %s %s: no offline eval bundle under %s\n", slug, language, cfg.EvalRoot)
					}
					if languageErr != nil {
						err = languageErr
					} else {
						var results []artifact.Result
						for _, language := range languages {
							var solved artifact.Result
							solved, err = engine(*cfg).Solve(ctx, p, solver.Options{Model: cfg.Model, Language: language, Candidates: cfg.Candidates, MaxRepairs: cfg.MaxRepairs, MaxCorrections: cfg.MaxCorrections, Force: *force, Submit: cfg.Session != "" && cfg.CSRFToken != ""})
							if err != nil {
								break
							}
							results = append(results, solved)
						}
						if err == nil && len(results) == 2 {
							_, err = (artifact.Store{Root: cfg.Output}).SaveCombined(p.Slug, results)
						}
					}
				}
				if err != nil {
					errs <- fmt.Errorf("%s: %w", slug, err)
				}
			}
		}()
	}
	count := 0
	for _, item := range items {
		if item.PaidOnly && !*includePaid {
			continue
		}
		if *limit > 0 && count >= *limit {
			break
		}
		jobs <- item.Slug
		count++
	}
	close(jobs)
	wg.Wait()
	close(errs)
	failed := 0
	for err := range errs {
		failed++
		fmt.Fprintln(os.Stderr, err)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d problems failed", failed, count)
	}
	fmt.Printf("accepted %d/%d problems\n", count, count)
	return nil
}

// chooseLanguages decides what to solve, and returns separately the languages it
// is declining to attempt. An article may exist only if its code passed a pinned
// offline bundle, so a language with no bundle on disk cannot produce one: in auto
// mode it is a reported gap rather than a guaranteed failure that would also throw
// away the languages that do have bundles. A language named explicitly is never
// dropped — the caller asked for it and deserves the bundle error.
func chooseLanguages(p problem.Problem, requested, evalRoot string) (languages, gaps []string, err error) {
	if requested != "auto" {
		if _, ok := p.Snippet(requested); ok {
			return []string{requested}, nil, nil
		}
		return nil, nil, fmt.Errorf("problem %s has no %q starter code", p.Slug, requested)
	}
	wanted := requiredLanguages(p)
	if len(wanted) == 0 {
		return nil, nil, fmt.Errorf("problem %s has no starter code", p.Slug)
	}
	for _, language := range wanted {
		if _, manifestErr := offline.ReadManifest(evalRoot, p.Slug, language); manifestErr != nil {
			gaps = append(gaps, language)
			continue
		}
		languages = append(languages, language)
	}
	if len(languages) == 0 {
		return nil, gaps, fmt.Errorf("problem %s has starter code for %s but no offline eval bundle for any of them, so no article could be proved correct; generate bundles with eval-sync", p.Slug, strings.Join(wanted, ", "))
	}
	return languages, gaps, nil
}

// requiredLanguages lists the implementations a complete answer owes, most
// preferred first, ignoring whether a bundle exists yet. Coverage is measured
// against this list, so a missing bundle counts against the repository instead of
// quietly shrinking what the repository claims to owe.
func requiredLanguages(p problem.Problem) []string {
	var wanted []string
	for _, language := range []string{"python3", "golang"} {
		if _, ok := p.Snippet(language); ok {
			wanted = append(wanted, language)
		}
	}
	if len(wanted) > 0 {
		return wanted
	}
	for _, language := range []string{"mysql", "bash", "java", "javascript"} {
		if _, ok := p.Snippet(language); ok {
			return []string{language}
		}
	}
	if len(p.Snippets) > 0 {
		return []string{p.Snippets[0].LanguageSlug}
	}
	return nil
}

func evaluate(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	input := fs.String("input", "", "JSONL containing task_id, dataset, and boolean results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("eval requires --input")
	}
	file, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	tasks, err := benchmark.ReadJSONL(file)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(benchmark.Summarize(tasks, []int{1, 5, 10}))
}

func verifyOffline(ctx context.Context, args []string) error {
	cfg := config.Load()
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	language := fs.String("language", "", "language slug")
	codePath := fs.String("code", "", "candidate code file")
	fs.StringVar(&cfg.EvalRoot, "eval-root", cfg.EvalRoot, "offline evaluation bundle root")
	fs.StringVar(&cfg.ContainerRuntime, "container-runtime", cfg.ContainerRuntime, "docker, podman, or auto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *language == "" || *codePath == "" {
		return errors.New("verify requires a problem slug, --language, and --code")
	}
	code, err := os.ReadFile(*codePath)
	if err != nil {
		return err
	}
	result, err := (offline.Runner{Root: cfg.EvalRoot, DockerBinary: cfg.ContainerRuntime}).Verify(ctx, fs.Arg(0), *language, string(code))
	_ = json.NewEncoder(os.Stdout).Encode(result)
	return err
}
