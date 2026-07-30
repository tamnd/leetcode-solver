package evaldataset

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// pyValue is a Python literal decoded into Go: nil, bool, int64, float64, string,
// or []pyValue for a list or tuple. A dataset assertion that needs anything richer
// is reported as a gap rather than approximated, because a bundle that silently
// drops a case would declare more tests than it can execute.
type pyValue any

// pyArg is one argument passed to the dataset's candidate. Name is empty when the
// dataset called the candidate positionally.
type pyArg struct {
	Name  string
	Value pyValue
}

// datasetCase is one executable comparison lifted out of a dataset check function:
// the arguments handed to the candidate and the value it must return.
type datasetCase struct {
	Args []pyArg
	Want pyValue
}

// extractCases lifts every `assert candidate(...) == <literal>` out of a dataset
// check function. It is deliberately all-or-nothing: if the body contains an
// assertion in any other shape — a tolerance comparison, a sorted() wrapper, a
// constructed object — the whole problem is refused. Emitting the subset it does
// understand would produce a bundle that proves less than it claims.
func extractCases(test string) ([]datasetCase, error) {
	body := stripComments(test)
	total := strings.Count(body, "assert ")
	if total == 0 {
		return nil, fmt.Errorf("check function contains no assertion")
	}
	var cases []datasetCase
	source := []rune(body)
	for offset := 0; ; {
		index := strings.Index(string(source[offset:]), "assert candidate(")
		if index < 0 {
			break
		}
		start := offset + index + len([]rune("assert candidate"))
		one, next, err := readCase(source, start)
		if err != nil {
			return nil, err
		}
		cases = append(cases, one)
		offset = next
	}
	if len(cases) != total {
		return nil, fmt.Errorf("only %d of %d assertions are a direct `assert candidate(...) == <literal>` comparison", len(cases), total)
	}
	return cases, nil
}

// readCase parses one assertion starting at the opening parenthesis of the
// candidate call and returns the offset just past the expected value.
func readCase(source []rune, open int) (datasetCase, int, error) {
	closing, err := matchParen(source, open)
	if err != nil {
		return datasetCase{}, 0, err
	}
	args, err := parseArgs(string(source[open+1 : closing]))
	if err != nil {
		return datasetCase{}, 0, err
	}
	rest := closing + 1
	for rest < len(source) && (source[rest] == ' ' || source[rest] == '\t') {
		rest++
	}
	if !strings.HasPrefix(string(source[rest:]), "==") {
		return datasetCase{}, 0, fmt.Errorf("assertion is not a direct equality comparison: %q", strings.TrimSpace(line(source, open)))
	}
	parser := &pyParser{src: source, pos: rest + 2}
	want, err := parser.next()
	if err != nil {
		return datasetCase{}, 0, fmt.Errorf("expected value of %q: %w", strings.TrimSpace(line(source, open)), err)
	}
	// Only horizontal space may follow the expected value. Skipping newlines here
	// would let `assert candidate(x) == 1 and x > 0` pass as a plain comparison.
	end := parser.pos
	for end < len(source) && (source[end] == ' ' || source[end] == '\t' || source[end] == '\r') {
		end++
	}
	if end < len(source) && source[end] != '\n' {
		return datasetCase{}, 0, fmt.Errorf("assertion has trailing expression: %q", strings.TrimSpace(line(source, open)))
	}
	return datasetCase{Args: args, Want: want}, parser.pos, nil
}

func line(source []rune, at int) string {
	start := at
	for start > 0 && source[start-1] != '\n' {
		start--
	}
	end := at
	for end < len(source) && source[end] != '\n' {
		end++
	}
	return string(source[start:end])
}

// stripComments removes trailing `#` comments, which would otherwise be mistaken
// for a trailing expression. Quotes are tracked so a `#` inside a string survives.
func stripComments(text string) string {
	var out strings.Builder
	for _, raw := range strings.Split(text, "\n") {
		source := []rune(raw)
		quote := rune(0)
		cut := len(source)
		for i := 0; i < len(source); i++ {
			switch {
			case quote != 0:
				if source[i] == '\\' {
					i++
				} else if source[i] == quote {
					quote = 0
				}
			case source[i] == '\'' || source[i] == '"':
				quote = source[i]
			case source[i] == '#':
				cut = i
				i = len(source)
			}
		}
		out.WriteString(strings.TrimRight(string(source[:cut]), " \t"))
		out.WriteByte('\n')
	}
	return out.String()
}

// matchParen finds the parenthesis closing the one at open, ignoring brackets that
// appear inside string literals.
func matchParen(source []rune, open int) (int, error) {
	if open >= len(source) || source[open] != '(' {
		return 0, fmt.Errorf("expected a call at offset %d", open)
	}
	depth, quote := 0, rune(0)
	for i := open; i < len(source); i++ {
		c := source[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unbalanced call at offset %d", open)
}

func parseArgs(text string) ([]pyArg, error) {
	var args []pyArg
	for _, part := range splitTopLevel(text) {
		if strings.TrimSpace(part) == "" {
			continue
		}
		name, expression := splitKeyword(part)
		value, err := parsePyLiteral(expression)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", strings.TrimSpace(part), err)
		}
		args = append(args, pyArg{Name: name, Value: value})
	}
	return args, nil
}

// splitKeyword separates `name = value` from a bare positional expression. Only a
// top-level `=` that is not part of a comparison operator names an argument.
func splitKeyword(part string) (name, expression string) {
	source := []rune(part)
	depth, quote := 0, rune(0)
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == '=' && depth == 0:
			if i+1 < len(source) && source[i+1] == '=' {
				return "", part
			}
			if i > 0 && strings.ContainsRune("!<>+-*/%", source[i-1]) {
				return "", part
			}
			candidate := strings.TrimSpace(string(source[:i]))
			if isIdentifier(candidate) {
				return candidate, string(source[i+1:])
			}
			return "", part
		}
	}
	return "", part
}

func isIdentifier(text string) bool {
	if text == "" {
		return false
	}
	for i, c := range text {
		if c == '_' || unicode.IsLetter(c) || (i > 0 && unicode.IsDigit(c)) {
			continue
		}
		return false
	}
	return true
}

func splitTopLevel(text string) []string {
	var parts []string
	source := []rune(text)
	depth, quote, start := 0, rune(0), 0
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, string(source[start:i]))
			start = i + 1
		}
	}
	return append(parts, string(source[start:]))
}

type pyParser struct {
	src []rune
	pos int
}

func parsePyLiteral(text string) (pyValue, error) {
	parser := &pyParser{src: []rune(text)}
	value, err := parser.next()
	if err != nil {
		return nil, err
	}
	parser.space()
	if parser.pos != len(parser.src) {
		return nil, fmt.Errorf("unsupported expression %q", strings.TrimSpace(text))
	}
	return value, nil
}

func (p *pyParser) space() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n' || p.src[p.pos] == '\r' || p.src[p.pos] == '\\') {
		p.pos++
	}
}

func (p *pyParser) next() (pyValue, error) {
	p.space()
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("expected a literal")
	}
	switch c := p.src[p.pos]; {
	case c == '[':
		return p.sequence(']')
	case c == '(':
		return p.sequence(')')
	case c == '\'' || c == '"':
		return p.text()
	case c == '-' || c == '+' || unicode.IsDigit(c):
		return p.number()
	default:
		for _, word := range []struct {
			literal string
			value   pyValue
		}{{"True", true}, {"False", false}, {"None", nil}} {
			if strings.HasPrefix(string(p.src[p.pos:]), word.literal) {
				p.pos += len([]rune(word.literal))
				return word.value, nil
			}
		}
		return nil, fmt.Errorf("unsupported expression starting at %q", strings.TrimSpace(string(p.src[p.pos:min(p.pos+24, len(p.src))])))
	}
}

func (p *pyParser) sequence(closer rune) (pyValue, error) {
	p.pos++
	items := []pyValue{}
	for {
		p.space()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unterminated sequence")
		}
		if p.src[p.pos] == closer {
			p.pos++
			return items, nil
		}
		item, err := p.next()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		p.space()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		}
	}
}

func (p *pyParser) text() (pyValue, error) {
	quote := p.src[p.pos]
	p.pos++
	var out strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '\\' && p.pos+1 < len(p.src) {
			escaped, ok := map[rune]rune{'n': '\n', 't': '\t', 'r': '\r', '\\': '\\', '\'': '\'', '"': '"', '0': 0}[p.src[p.pos+1]]
			if !ok {
				return nil, fmt.Errorf("unsupported escape \\%c", p.src[p.pos+1])
			}
			out.WriteRune(escaped)
			p.pos += 2
			continue
		}
		if c == quote {
			p.pos++
			return out.String(), nil
		}
		out.WriteRune(c)
		p.pos++
	}
	return nil, fmt.Errorf("unterminated string")
}

func (p *pyParser) number() (pyValue, error) {
	start := p.pos
	if p.src[p.pos] == '-' || p.src[p.pos] == '+' {
		p.pos++
	}
	float := false
digits:
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case unicode.IsDigit(c) || c == '_':
			p.pos++
		case c == '.':
			float = true
			p.pos++
		case c == 'e' || c == 'E':
			float = true
			p.pos++
			if p.pos < len(p.src) && (p.src[p.pos] == '-' || p.src[p.pos] == '+') {
				p.pos++
			}
		default:
			break digits
		}
	}
	text := strings.ReplaceAll(string(p.src[start:p.pos]), "_", "")
	if float {
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, err
	}
	return value, nil
}
