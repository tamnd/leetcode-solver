// Package agentbench prepares and reports reproducible, offline agent benchmarks.
package agentbench

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
	"strings"
	"time"
)

const (
	DatasetRevision  = "0fe84c3912ea0c4d4a78037083943e8f0c4dd505"
	DatasetSHA256    = "bb4c364f71921c4495a6ad15abe1a927350b720009f4933e2e71f8af0f6fd1f5"
	DatasetFile      = "test6.jsonl"
	TomoRevision     = "a999294f812f79b2daac681650c32b638525e2bf"
	TomoLabsRevision = "c6808df75d9441a75e250f7806ce56cbed2d9dab"
)

func DatasetURL(revision string) string {
	return "https://huggingface.co/datasets/livecodebench/code_generation_lite/resolve/" + revision + "/" + DatasetFile
}

type Row struct {
	QuestionTitle, QuestionContent, Platform, QuestionID string
	ContestID, ContestDate, StarterCode, Difficulty      string
	PublicTestCases, PrivateTestCases, Metadata          string
}

func (r *Row) UnmarshalJSON(data []byte) error {
	type wire struct {
		QuestionTitle    string `json:"question_title"`
		QuestionContent  string `json:"question_content"`
		Platform         string `json:"platform"`
		QuestionID       string `json:"question_id"`
		ContestID        string `json:"contest_id"`
		ContestDate      string `json:"contest_date"`
		StarterCode      string `json:"starter_code"`
		Difficulty       string `json:"difficulty"`
		PublicTestCases  string `json:"public_test_cases"`
		PrivateTestCases string `json:"private_test_cases"`
		Metadata         string `json:"metadata"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*r = Row{w.QuestionTitle, w.QuestionContent, w.Platform, w.QuestionID, w.ContestID, w.ContestDate, w.StarterCode, w.Difficulty, w.PublicTestCases, w.PrivateTestCases, w.Metadata}
	return nil
}

func (r Row) FunctionName() string {
	var m struct {
		FuncName string `json:"func_name"`
	}
	_ = json.Unmarshal([]byte(r.Metadata), &m)
	return m.FuncName
}

func Sync(ctx context.Context, cacheDir string, offline bool, progress func(string)) (string, error) {
	if cacheDir == "" {
		return "", errors.New("cache directory is required")
	}
	path := filepath.Join(cacheDir, DatasetRevision, DatasetFile)
	if validFile(path, DatasetSHA256) {
		return path, nil
	}
	if offline {
		return "", fmt.Errorf("offline dataset cache missing or corrupt: %s", path)
	}
	if progress != nil {
		progress("downloading pinned LiveCodeBench v6 dataset")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp := path + ".part"
	_ = os.Remove(tmp)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DatasetURL(DatasetRevision), nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 45 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download dataset: %s", resp.Status)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != DatasetSHA256 {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("dataset checksum: got %s want %s", got, DatasetSHA256)
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

func validFile(path, want string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == want
}

// SelectLatest chooses the newest functional LeetCode problem in each official
// difficulty. Private tests remain opaque strings and are never included in prompts.
func SelectLatest(path string, maxPrivateBytes int) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if maxPrivateBytes <= 0 {
		maxPrivateBytes = 1 << 20
	}
	latest := map[string]Row{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64<<10), 32<<20)
	for s.Scan() {
		var row Row
		if json.Unmarshal(s.Bytes(), &row) != nil || !strings.EqualFold(row.Platform, "leetcode") || row.FunctionName() == "" || strings.TrimSpace(row.QuestionContent) == "" || len(row.PrivateTestCases) > maxPrivateBytes {
			continue
		}
		d := strings.ToLower(strings.TrimSpace(row.Difficulty))
		if d != "easy" && d != "medium" && d != "hard" {
			continue
		}
		if old, ok := latest[d]; !ok || row.ContestDate > old.ContestDate || (row.ContestDate == old.ContestDate && row.QuestionID > old.QuestionID) {
			latest[d] = row
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	var out []Row
	for _, d := range []string{"easy", "medium", "hard"} {
		row, ok := latest[d]
		if !ok {
			return nil, fmt.Errorf("dataset has no eligible %s LeetCode problem", d)
		}
		out = append(out, row)
	}
	return out, nil
}
