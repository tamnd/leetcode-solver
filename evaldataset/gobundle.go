package evaldataset

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/tamnd/leetcode-solver/offline"
)

// goParam is one flattened parameter of a LeetCode Go starter signature.
type goParam struct {
	Name string
	Type string
}

// goSignature is the shape a generated driver must call.
type goSignature struct {
	Name   string
	Params []goParam
	Result string
}

// parseGoSignature reads a LeetCode Go starter snippet. Only a single free function
// with one return value can be driven by a table of literals: a method set, a
// constructor, or a mutate-in-place signature needs a different harness, and is
// refused here so the caller can report it rather than emit a bundle that will not
// compile inside the container.
func parseGoSignature(snippet string) (goSignature, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "solution.go", "package main\n\n"+snippet+"\n", parser.SkipObjectResolution)
	if err != nil {
		return goSignature{}, fmt.Errorf("starter code does not parse: %w", err)
	}
	if len(file.Decls) != 1 {
		return goSignature{}, fmt.Errorf("starter code declares %d top-level items, want exactly one function", len(file.Decls))
	}
	decl, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok {
		return goSignature{}, fmt.Errorf("starter code does not declare a function")
	}
	if decl.Recv != nil {
		return goSignature{}, fmt.Errorf("starter code declares a method, which needs a stateful harness")
	}
	if decl.Type.TypeParams != nil {
		return goSignature{}, fmt.Errorf("starter code is generic")
	}
	if decl.Type.Results == nil || len(decl.Type.Results.List) != 1 || len(decl.Type.Results.List[0].Names) > 1 {
		return goSignature{}, fmt.Errorf("starter code does not return exactly one value")
	}
	signature := goSignature{Name: decl.Name.Name, Result: types.ExprString(decl.Type.Results.List[0].Type)}
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			rendered := types.ExprString(field.Type)
			if len(field.Names) == 0 {
				return goSignature{}, fmt.Errorf("parameter of type %s is unnamed", rendered)
			}
			for _, name := range field.Names {
				signature.Params = append(signature.Params, goParam{Name: name.Name, Type: rendered})
			}
		}
	}
	return signature, nil
}

// orderArgs matches the arguments the dataset passed against the Go signature. The
// dataset names its arguments, and LeetCode uses the same names in both languages,
// so a name that does not appear in the Go signature means the two sides disagree
// about the problem and the bundle must not be generated.
func orderArgs(signature goSignature, args []pyArg) ([]pyValue, error) {
	if len(args) != len(signature.Params) {
		return nil, fmt.Errorf("dataset passes %d arguments but %s takes %d", len(args), signature.Name, len(signature.Params))
	}
	ordered := make([]pyValue, len(signature.Params))
	filled := make([]bool, len(signature.Params))
	for position, arg := range args {
		index := position
		if arg.Name != "" {
			index = -1
			for i, param := range signature.Params {
				if param.Name == arg.Name {
					index = i
					break
				}
			}
			if index < 0 {
				return nil, fmt.Errorf("dataset argument %q is not a parameter of %s", arg.Name, signature.Name)
			}
		}
		if filled[index] {
			return nil, fmt.Errorf("dataset passes %s twice", signature.Params[index].Name)
		}
		ordered[index], filled[index] = arg.Value, true
	}
	return ordered, nil
}

// renderGo writes a Python literal as Go source of exactly the declared type. It
// refuses anything it cannot render faithfully, so an unsupported problem fails at
// import time with a named reason instead of inside the container later.
func renderGo(goType string, value pyValue) (string, error) {
	switch goType {
	case "int", "int32", "int64", "uint", "uint32", "uint64":
		number, ok := value.(int64)
		if !ok {
			return "", typeMismatch(goType, value)
		}
		return strconv.FormatInt(number, 10), nil
	case "float64", "float32":
		switch number := value.(type) {
		case float64:
			return strconv.FormatFloat(number, 'g', -1, 64), nil
		case int64:
			return strconv.FormatInt(number, 10), nil
		}
		return "", typeMismatch(goType, value)
	case "string":
		text, ok := value.(string)
		if !ok {
			return "", typeMismatch(goType, value)
		}
		return strconv.Quote(text), nil
	case "bool":
		flag, ok := value.(bool)
		if !ok {
			return "", typeMismatch(goType, value)
		}
		return strconv.FormatBool(flag), nil
	case "byte", "uint8", "rune", "int8":
		switch item := value.(type) {
		case string:
			runes := []rune(item)
			if len(runes) != 1 {
				return "", fmt.Errorf("cannot render %q as a single %s", item, goType)
			}
			return strconv.QuoteRune(runes[0]), nil
		case int64:
			return strconv.FormatInt(item, 10), nil
		}
		return "", typeMismatch(goType, value)
	}
	if element, ok := strings.CutPrefix(goType, "[]"); ok {
		// A Python string standing in for a Go []byte or []rune is the one implicit
		// conversion LeetCode itself makes, so it is honoured explicitly.
		if text, isText := value.(string); isText {
			switch element {
			case "byte", "uint8", "rune", "int32":
				return goType + "(" + strconv.Quote(text) + ")", nil
			}
			return "", typeMismatch(goType, value)
		}
		items, isList := value.([]pyValue)
		if !isList {
			return "", typeMismatch(goType, value)
		}
		rendered := make([]string, len(items))
		for i, item := range items {
			part, err := renderGo(element, item)
			if err != nil {
				return "", err
			}
			rendered[i] = part
		}
		return goType + "{" + strings.Join(rendered, ", ") + "}", nil
	}
	return "", fmt.Errorf("unsupported Go type %s", goType)
}

func typeMismatch(goType string, value pyValue) error {
	return fmt.Errorf("cannot render %T value %v as Go type %s", value, value, goType)
}

// goHarness renders a table-driven test that calls the candidate once per dataset
// case and prints the completion marker with the number of comparisons it actually
// performed. The marker is printed from a deferred call so that a candidate that
// panics or fails an assertion still reports how far it got.
func goHarness(signature goSignature, cases []datasetCase) (string, error) {
	fields := make([]string, 0, len(signature.Params)+1)
	arguments := make([]string, 0, len(signature.Params))
	for i, param := range signature.Params {
		fields = append(fields, fmt.Sprintf("\t\targ%d %s", i, param.Type))
		arguments = append(arguments, fmt.Sprintf("tc.arg%d", i))
	}
	fields = append(fields, fmt.Sprintf("\t\twant %s", signature.Result))
	rows := make([]string, 0, len(cases))
	for _, one := range cases {
		ordered, err := orderArgs(signature, one.Args)
		if err != nil {
			return "", err
		}
		values := make([]string, 0, len(ordered)+1)
		for i, value := range ordered {
			rendered, err := renderGo(signature.Params[i].Type, value)
			if err != nil {
				return "", err
			}
			values = append(values, rendered)
		}
		want, err := renderGo(signature.Result, one.Want)
		if err != nil {
			return "", err
		}
		values = append(values, want)
		rows = append(rows, "\t\t{"+strings.Join(values, ", ")+"},")
	}
	comparison, extra := "!reflect.DeepEqual(got, tc.want)", "reflect"
	if signature.Result == "float64" || signature.Result == "float32" {
		// Comparing floats bit for bit would reject a correct candidate that reached
		// the same answer through a different order of operations.
		comparison, extra = "math.Abs(float64(got-tc.want)) > 1e-6", "math"
	}
	return `package main

import (
	"fmt"
	"` + extra + `"
	"testing"
)

// TestCandidate replays every case the pinned dataset states for this problem.
func TestCandidate(t *testing.T) {
	ran := 0
	defer func() { fmt.Printf("` + offline.PassMarker + ` ` + offline.NoncePlaceholder + ` assertions=%d\n", ran) }()
	cases := []struct {
` + strings.Join(fields, "\n") + `
	}{
` + strings.Join(rows, "\n") + `
	}
	for index, tc := range cases {
		got := ` + signature.Name + `(` + strings.Join(arguments, ", ") + `)
		if ` + comparison + ` {
			t.Fatalf("case %d: ` + signature.Name + ` returned %v, want %v", index, got, tc.want)
		}
		ran++
	}
}
`, nil
}

// goManifest describes how the container runs the rendered driver. `go test` is
// given -v because cmd/go prints only "ok <package>" for a passing package, which
// would swallow the completion marker.
func goManifest(slug, image, dataset, revision string, tests int) offline.Manifest {
	return offline.Manifest{
		SchemaVersion:   offline.SchemaVersion,
		ProblemSlug:     slug,
		Language:        "golang",
		Dataset:         dataset,
		Revision:        revision,
		Image:           image,
		CandidateFile:   "solution.go",
		CandidatePrefix: "package main\n\n",
		Command:         []string{"go", "test", "-v", "-count=1", "solution.go", "solution_test.go"},
		Files:           []string{"solution_test.go"},
		TestCount:       tests,
		TimeoutSeconds:  120,
	}
}
