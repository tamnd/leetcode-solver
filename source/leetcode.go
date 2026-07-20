// Package source loads the complete LeetCode catalog and individual statements.
package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tamnd/leetcode-solver/problem"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Session    string
	CSRFToken  string
	UserAgent  string
}

func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://leetcode.com"
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: &http.Client{Timeout: 30 * time.Second}, UserAgent: "leetcode-solver/0.1"}
}

type CatalogItem struct {
	ID         string          `json:"questionId"`
	FrontendID string          `json:"questionFrontendId"`
	Title      string          `json:"title"`
	Slug       string          `json:"titleSlug"`
	Difficulty string          `json:"difficulty"`
	PaidOnly   bool            `json:"isPaidOnly"`
	Topics     []problem.Topic `json:"topicTags"`
}

const catalogQuery = `query problemsetQuestionList($categorySlug:String,$limit:Int,$skip:Int,$filters:QuestionListFilterInput){problemsetQuestionList:questionList(categorySlug:$categorySlug,limit:$limit,skip:$skip,filters:$filters){total:totalNum,questions:data{questionId,questionFrontendId,title,titleSlug,difficulty,isPaidOnly,topicTags{name,slug}}}}`
const questionQuery = `query questionData($titleSlug:String!){question(titleSlug:$titleSlug){questionId,questionFrontendId,title,titleSlug,difficulty,isPaidOnly,content,translatedTitle,exampleTestcases,metaData,hints,topicTags{name,slug},codeSnippets{lang,langSlug,code}}}`

func (c *Client) Catalog(ctx context.Context) ([]CatalogItem, error) {
	const pageSize = 100
	var all []CatalogItem
	for skip := 0; ; skip += pageSize {
		var response struct {
			Data struct {
				List struct {
					Total     int           `json:"total"`
					Questions []CatalogItem `json:"questions"`
				} `json:"problemsetQuestionList"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := c.graphql(ctx, catalogQuery, map[string]any{"categorySlug": "", "limit": pageSize, "skip": skip, "filters": map[string]any{}}, &response); err != nil {
			return nil, err
		}
		if len(response.Errors) > 0 {
			return nil, errors.New(response.Errors[0].Message)
		}
		all = append(all, response.Data.List.Questions...)
		if len(all) >= response.Data.List.Total || len(response.Data.List.Questions) == 0 {
			return all, nil
		}
	}
}

func (c *Client) Problem(ctx context.Context, slug string) (problem.Problem, error) {
	var response struct {
		Data struct {
			Question struct {
				ID               string          `json:"questionId"`
				FrontendID       string          `json:"questionFrontendId"`
				Title            string          `json:"title"`
				Slug             string          `json:"titleSlug"`
				Difficulty       string          `json:"difficulty"`
				PaidOnly         bool            `json:"isPaidOnly"`
				Content          string          `json:"content"`
				TranslatedTitle  string          `json:"translatedTitle"`
				ExampleTestcases string          `json:"exampleTestcases"`
				MetaData         string          `json:"metaData"`
				Hints            []string        `json:"hints"`
				Topics           []problem.Topic `json:"topicTags"`
				Snippets         []struct {
					Language     string `json:"lang"`
					LanguageSlug string `json:"langSlug"`
					Code         string `json:"code"`
				} `json:"codeSnippets"`
			} `json:"question"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.graphql(ctx, questionQuery, map[string]any{"titleSlug": slug}, &response); err != nil {
		return problem.Problem{}, err
	}
	if len(response.Errors) > 0 {
		return problem.Problem{}, errors.New(response.Errors[0].Message)
	}
	q := response.Data.Question
	if q.ID == "" {
		return problem.Problem{}, fmt.Errorf("problem %q not found", slug)
	}
	snippets := make([]problem.CodeSnippet, len(q.Snippets))
	for i, s := range q.Snippets {
		snippets[i] = problem.CodeSnippet{Language: s.Language, LanguageSlug: s.LanguageSlug, Code: s.Code}
	}
	return problem.Problem{ID: q.ID, FrontendID: q.FrontendID, Title: q.Title, Slug: q.Slug, Difficulty: q.Difficulty, PaidOnly: q.PaidOnly, ContentHTML: q.Content, TranslatedTitle: q.TranslatedTitle, ExampleTestcases: q.ExampleTestcases, MetaData: q.MetaData, Hints: q.Hints, Topics: q.Topics, Snippets: snippets, UpdatedAt: time.Now().UTC()}, nil
}

func (c *Client) graphql(ctx context.Context, query string, variables map[string]any, target any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Session != "" {
		req.AddCookie(&http.Cookie{Name: "LEETCODE_SESSION", Value: c.Session})
	}
	if c.CSRFToken != "" {
		req.AddCookie(&http.Cookie{Name: "csrftoken", Value: c.CSRFToken})
		req.Header.Set("X-CSRFToken", c.CSRFToken)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("leetcode graphql: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	return nil
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}
