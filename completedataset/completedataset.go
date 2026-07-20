// Package completedataset downloads and imports the pinned
// whiskwhite/leetcode-complete snapshot using Go-only tooling.
package completedataset

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/tamnd/leetcode-solver/problem"
	"github.com/tamnd/leetcode-solver/source"
)

const (
	Source          = "whiskwhite/leetcode-complete"
	DefaultRevision = "34a722638c97984c161a3ba9f60ed99c3f07f734"
)

type File struct {
	Split, Name, SHA256 string
}

var DefaultFiles = []File{
	{Split: "train", Name: "train.jsonl", SHA256: "8529d4ae8bd80a2b3b73b261a756116b92f1f44f2c85f11a2182d85c2e98693d"},
	{Split: "validation", Name: "validation.jsonl", SHA256: "df767eec8b8b9a691e9eb40b8c325deeeb85ae6f466e0701491348f78cff9da0"},
	{Split: "test", Name: "test.jsonl", SHA256: "a6067f5b7a92248f1d31192ddf51593997230dd2aa00e7c21fb92385b5cafd33"},
	{Split: "unsolved", Name: "unsolved.jsonl", SHA256: "bfcc2c2d23e5d22593d261a19dc01970d7b0160b52318770d23c8a1de3cccd7a"},
}

type Options struct {
	Revision, CacheDir string
	Offline            bool
	HTTPClient         *http.Client
	Progress           func(message string)
}

type Report struct {
	Rows             int            `json:"source_rows"`
	UniqueProblems   int            `json:"unique_problems"`
	DuplicateIDs     int            `json:"duplicate_ids"`
	Solutions        int            `json:"solutions"`
	PythonSolutions  int            `json:"python_solutions"`
	GoSolutions      int            `json:"go_solutions"`
	WithoutSolutions int            `json:"without_solutions"`
	Splits           map[string]int `json:"splits"`
}

type row struct {
	AcceptanceRate  float64         `json:"acceptance_rate"`
	Category        string          `json:"category"`
	CodeSnippets    []snippet       `json:"code_snippets"`
	Content         string          `json:"content"`
	CreatedAt       string          `json:"created_at_approx"`
	Difficulty      string          `json:"difficulty"`
	Dislikes        int64           `json:"dislikes"`
	ExampleTestCase string          `json:"example_test_cases"`
	FrontendID      string          `json:"frontend_id"`
	ID              string          `json:"id"`
	PaidOnly        bool            `json:"is_paid_only"`
	Likes           int64           `json:"likes"`
	Solutions       json.RawMessage `json:"solutions"`
	Title           string          `json:"title"`
	Slug            string          `json:"title_slug"`
	Topics          []string        `json:"topic_tags"`
	TotalAccepted   int64           `json:"total_accepted"`
	TotalSubmitted  int64           `json:"total_submissions"`
	URL             string          `json:"url"`
}

type snippet struct {
	Code string `json:"code"`
	Lang string `json:"lang"`
}

type solution struct {
	Lang string `json:"lang"`
}

type metadata struct {
	Source            string   `json:"source"`
	Revision          string   `json:"revision"`
	Split             string   `json:"split"`
	Category          string   `json:"category"`
	AcceptanceRate    float64  `json:"acceptance_rate"`
	CreatedAt         string   `json:"created_at_approx"`
	Likes             int64    `json:"likes"`
	Dislikes          int64    `json:"dislikes"`
	TotalAccepted     int64    `json:"total_accepted"`
	TotalSubmitted    int64    `json:"total_submissions"`
	URL               string   `json:"url"`
	SolutionLanguages []string `json:"solution_languages"`
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func Sync(ctx context.Context, database *source.SQLite, options Options) (Report, error) {
	var report Report
	report.Splits = make(map[string]int)
	if database == nil {
		return report, errors.New("SQLite database is required")
	}
	if options.Revision == "" {
		options.Revision = DefaultRevision
	}
	if options.Revision != DefaultRevision {
		return report, fmt.Errorf("revision %s has no reviewed checksum manifest; update DefaultFiles before importing it", options.Revision)
	}
	if options.CacheDir == "" {
		return report, errors.New("cache directory is required")
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	seenIDs := make(map[string]bool)
	for _, file := range DefaultFiles {
		path, err := ensureFile(ctx, client, options, file)
		if err != nil {
			return report, err
		}
		if options.Progress != nil {
			options.Progress("importing " + file.Split)
		}
		part, err := importFile(ctx, database, path, options.Revision, file.Split, seenIDs)
		if err != nil {
			return report, err
		}
		report.Rows += part.Rows
		report.UniqueProblems += part.UniqueProblems
		report.DuplicateIDs += part.DuplicateIDs
		report.Solutions += part.Solutions
		report.PythonSolutions += part.PythonSolutions
		report.GoSolutions += part.GoSolutions
		report.WithoutSolutions += part.WithoutSolutions
		report.Splits[file.Split] = part.Rows
	}
	return report, nil
}

func ensureFile(ctx context.Context, client *http.Client, options Options, file File) (string, error) {
	dir := filepath.Join(options.CacheDir, options.Revision)
	path := filepath.Join(dir, file.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		if err := verifyFile(path, file.SHA256); err != nil {
			return "", fmt.Errorf("cached %s failed verification: %w", path, err)
		}
		if options.Progress != nil {
			options.Progress("using verified cache " + path)
		}
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if options.Offline {
		return "", fmt.Errorf("offline import requires verified cached file %s", path)
	}
	url := fmt.Sprintf("https://huggingface.co/datasets/%s/resolve/%s/%s", Source, options.Revision, file.Name)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "leetcode-solver/0.1")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("download %s: %s: %s", file.Name, response.Status, strings.TrimSpace(string(body)))
	}
	temporary, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temporary, hash), response.Body)
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != file.SHA256 {
		return "", fmt.Errorf("download %s sha256=%s, want %s", file.Name, got, file.SHA256)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func verifyFile(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("sha256=%s, want %s", got, want)
	}
	return nil
}

func importFile(ctx context.Context, database *source.SQLite, path, revision, split string, seenIDs map[string]bool) (Report, error) {
	var report Report
	if seenIDs == nil {
		seenIDs = make(map[string]bool)
	}
	file, err := os.Open(path)
	if err != nil {
		return report, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var value row
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			return report, fmt.Errorf("decode %s row %d: %w", split, report.Rows+1, err)
		}
		if !safeID.MatchString(value.ID) || !safeID.MatchString(value.Slug) {
			return report, fmt.Errorf("unsafe dataset identity %q/%q", value.ID, value.Slug)
		}
		if seenIDs[value.ID] {
			report.DuplicateIDs++
		} else {
			seenIDs[value.ID] = true
			report.UniqueProblems++
		}
		solutions := value.Solutions
		if len(solutions) == 0 || string(solutions) == "null" {
			solutions = json.RawMessage("[]")
		}
		var decodedSolutions []solution
		if err := json.Unmarshal(solutions, &decodedSolutions); err != nil {
			return report, fmt.Errorf("decode solutions for %s: %w", value.Slug, err)
		}
		languages := uniqueLanguages(decodedSolutions)
		meta, err := json.Marshal(metadata{Source: Source, Revision: revision, Split: split, Category: value.Category, AcceptanceRate: value.AcceptanceRate, CreatedAt: value.CreatedAt, Likes: value.Likes, Dislikes: value.Dislikes, TotalAccepted: value.TotalAccepted, TotalSubmitted: value.TotalSubmitted, URL: value.URL, SolutionLanguages: languages})
		if err != nil {
			return report, err
		}
		importedAt := time.Now().UTC()
		p := problem.Problem{ID: value.ID, FrontendID: value.FrontendID, Slug: value.Slug, Title: value.Title, Difficulty: value.Difficulty, PaidOnly: value.PaidOnly, ContentHTML: value.Content, ExampleTestcases: value.ExampleTestCase, SampleTestcase: firstLine(value.ExampleTestCase), MetaData: string(meta), UpdatedAt: importedAt}
		for _, item := range value.CodeSnippets {
			p.Snippets = append(p.Snippets, problem.CodeSnippet{Language: languageName(item.Lang), LanguageSlug: item.Lang, Code: item.Code})
		}
		for _, topic := range value.Topics {
			p.Topics = append(p.Topics, problem.Topic{Name: topic, Slug: slugify(topic)})
		}
		if err := database.Put(ctx, p); err != nil {
			return report, fmt.Errorf("store problem %s: %w", value.Slug, err)
		}
		if err := database.PutReferenceData(ctx, source.ReferenceData{Source: Source, Revision: revision, QuestionID: value.ID, Slug: value.Slug, SolutionsJSON: solutions, MetadataJSON: meta, ImportedAt: importedAt}); err != nil {
			return report, fmt.Errorf("store references for %s: %w", value.Slug, err)
		}
		report.Rows++
		report.Solutions += len(decodedSolutions)
		if len(decodedSolutions) == 0 {
			report.WithoutSolutions++
		}
		for _, item := range decodedSolutions {
			switch item.Lang {
			case "python3":
				report.PythonSolutions++
			case "golang":
				report.GoSolutions++
			}
		}
	}
	return report, scanner.Err()
}

func uniqueLanguages(solutions []solution) []string {
	seen := make(map[string]bool)
	var languages []string
	for _, item := range solutions {
		if item.Lang != "" && !seen[item.Lang] {
			seen[item.Lang] = true
			languages = append(languages, item.Lang)
		}
	}
	return languages
}

func languageName(slug string) string {
	switch slug {
	case "python3":
		return "Python3"
	case "golang":
		return "Go"
	default:
		return slug
	}
}

func slugify(text string) string {
	var result strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
			dash = false
		} else if !dash && result.Len() > 0 {
			result.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}
