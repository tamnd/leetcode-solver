package eval

import (
	"math"
	"strings"
	"testing"
)

func TestEstimatePassAtK(t *testing.T) {
	if got := EstimatePassAtK(10, 1, 1); math.Abs(got-.1) > 1e-12 {
		t.Fatalf("got %f", got)
	}
	if got := EstimatePassAtK(10, 2, 10); got != 1 {
		t.Fatalf("got %f", got)
	}
}
func TestReadAndSummarize(t *testing.T) {
	tasks, err := ReadJSONL(strings.NewReader("{\"task_id\":\"a\",\"dataset\":\"test\",\"results\":[true,false]}\n{\"task_id\":\"b\",\"dataset\":\"test\",\"results\":[false,false]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	report := Summarize(tasks, []int{1, 2})
	if report.Tasks != 2 || report.Passed != 1 {
		t.Fatalf("%+v", report)
	}
	if math.Abs(report.PassAtK["pass@1"]-.25) > 1e-12 {
		t.Fatalf("%+v", report.PassAtK)
	}
}
