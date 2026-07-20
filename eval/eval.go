// Package eval reports execution-based benchmark quality with unbiased pass@k.
package eval

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

type Task struct {
	TaskID      string `json:"task_id"`
	Dataset     string `json:"dataset"`
	ReleaseDate string `json:"release_date,omitempty"`
	Results     []bool `json:"results"`
}
type Report struct {
	Dataset string             `json:"dataset"`
	Tasks   int                `json:"tasks"`
	Samples int                `json:"samples"`
	Passed  int                `json:"passed"`
	PassAtK map[string]float64 `json:"pass_at_k"`
}

func ReadJSONL(r io.Reader) ([]Task, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var tasks []Task
	for scanner.Scan() {
		var task Task
		if err := json.Unmarshal(scanner.Bytes(), &task); err != nil {
			return nil, fmt.Errorf("decode benchmark row %d: %w", len(tasks)+1, err)
		}
		if task.TaskID == "" || len(task.Results) == 0 {
			return nil, fmt.Errorf("benchmark row %d requires task_id and results", len(tasks)+1)
		}
		tasks = append(tasks, task)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, errors.New("benchmark is empty")
	}
	return tasks, nil
}

func Summarize(tasks []Task, ks []int) Report {
	report := Report{Dataset: tasks[0].Dataset, Tasks: len(tasks), PassAtK: map[string]float64{}}
	for _, task := range tasks {
		report.Samples += len(task.Results)
		for _, passed := range task.Results {
			if passed {
				report.Passed++
			}
		}
	}
	for _, k := range ks {
		var sum float64
		var count int
		for _, task := range tasks {
			n := len(task.Results)
			if n < k {
				continue
			}
			c := 0
			for _, passed := range task.Results {
				if passed {
					c++
				}
			}
			sum += EstimatePassAtK(n, c, k)
			count++
		}
		if count > 0 {
			report.PassAtK[fmt.Sprintf("pass@%d", k)] = sum / float64(count)
		}
	}
	return report
}

// EstimatePassAtK is 1-C(n-c,k)/C(n,k), evaluated as a stable product.
func EstimatePassAtK(n, c, k int) float64 {
	if n <= 0 || c < 0 || c > n || k <= 0 || k > n {
		return math.NaN()
	}
	if n-c < k {
		return 1
	}
	failure := 1.0
	for i := 0; i < k; i++ {
		failure *= float64(n-c-i) / float64(n-i)
	}
	return 1 - failure
}
