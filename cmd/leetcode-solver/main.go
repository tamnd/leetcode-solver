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
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/tamnd/leetcode-solver/artifact"
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
	case "version":
		fmt.Println(version)
		return nil
	default:
		return usage()
	}
}
func usage() error {
	fmt.Fprintln(os.Stderr, "usage: leetcode-solver <sync|complete-sync|convert|reference|eval-sync|coverage|solve|batch|catalog|verify|eval|version> [options]")
	return errors.New("command is required")
}

func flags(name string) (*flag.FlagSet, *config.Config, *bool) {
	cfg := config.Load()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	force := fs.Bool("force", false, "ignore an accepted cached result")
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
	fs.IntVar(&cfg.MaxRepairs, "max-repairs", cfg.MaxRepairs, "judge-guided repair attempts")
	return fs, &cfg, force
}
func engine(cfg config.Config) *solver.Engine {
	return &solver.Engine{Client: &llm.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey}, Judge: &judge.LeetCode{BaseURL: cfg.LeetCodeURL, Session: cfg.Session, CSRFToken: cfg.CSRFToken}, Offline: &offline.Runner{Root: cfg.EvalRoot, DockerBinary: cfg.ContainerRuntime}, Store: artifact.Store{Root: cfg.Output}, Progress: func(message string) { fmt.Fprintln(os.Stderr, message) }}
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
	fs.StringVar(&cfg.EvalRoot, "eval-root", cfg.EvalRoot, "offline evaluation bundle root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	totalRows, totalBundles, totalTests := 0, 0, 0
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
		report, importErr := evaldataset.ImportLeetCodeDataset(ctx, response.Body, cfg.EvalRoot, *image, "hf:"+*revision+":"+*versionName)
		_ = response.Body.Close()
		if importErr != nil {
			return importErr
		}
		totalRows += report.Rows
		totalBundles += report.Bundles
		totalTests += report.Tests
		fmt.Fprintf(os.Stderr, "imported %s: %d rows, %d bundles, %d tests, %d missing\n", split, report.Rows, report.Bundles, report.Tests, len(report.MissingTests))
	}
	fmt.Printf("read %d rows; imported %d Python bundles with %d offline tests\n", totalRows, totalBundles, totalTests)
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
		languages, languageErr := chooseLanguages(p, cfg.Language)
		if languageErr != nil {
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
	repository, err := source.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() { _ = repository.Close() }()
	p, err := repository.Problem(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	languages, err := chooseLanguages(p, cfg.Language)
	if err != nil {
		return err
	}
	var results []artifact.Result
	for _, language := range languages {
		result, solveErr := engine(*cfg).Solve(ctx, p, solver.Options{Model: cfg.Model, Language: language, Candidates: cfg.Candidates, MaxRepairs: cfg.MaxRepairs, Force: *force, Submit: cfg.Session != "" && cfg.CSRFToken != ""})
		if solveErr != nil {
			return fmt.Errorf("%s: %w", language, solveErr)
		}
		fmt.Printf("accepted offline: %s (%d), %s, %d tests", p.Title, pID(p.FrontendID), language, result.Offline.TestCount)
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
					languages, languageErr := chooseLanguages(p, cfg.Language)
					if languageErr != nil {
						err = languageErr
					} else {
						var results []artifact.Result
						for _, language := range languages {
							var solved artifact.Result
							solved, err = engine(*cfg).Solve(ctx, p, solver.Options{Model: cfg.Model, Language: language, Candidates: cfg.Candidates, MaxRepairs: cfg.MaxRepairs, Force: *force, Submit: cfg.Session != "" && cfg.CSRFToken != ""})
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

func chooseLanguages(p problem.Problem, requested string) ([]string, error) {
	if requested != "auto" {
		if _, ok := p.Snippet(requested); ok {
			return []string{requested}, nil
		}
		return nil, fmt.Errorf("problem %s has no %q starter code", p.Slug, requested)
	}
	_, hasPython := p.Snippet("python3")
	_, hasGo := p.Snippet("golang")
	if hasPython && hasGo {
		return []string{"python3", "golang"}, nil
	}
	for _, language := range []string{"python3", "golang", "mysql", "bash", "java", "javascript"} {
		if _, ok := p.Snippet(language); ok {
			return []string{language}, nil
		}
	}
	if len(p.Snippets) > 0 {
		return []string{p.Snippets[0].LanguageSlug}, nil
	}
	return nil, fmt.Errorf("problem %s has no starter code", p.Slug)
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
