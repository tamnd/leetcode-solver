# leetcode-solver

[![CI](https://github.com/tamnd/leetcode-solver/actions/workflows/ci.yml/badge.svg)](https://github.com/tamnd/leetcode-solver/actions/workflows/ci.yml)
[![CodeQL](https://github.com/tamnd/leetcode-solver/actions/workflows/codeql.yml/badge.svg)](https://github.com/tamnd/leetcode-solver/actions/workflows/codeql.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/tamnd/leetcode-solver.svg)](https://pkg.go.dev/github.com/tamnd/leetcode-solver)
[![License](https://img.shields.io/github/license/tamnd/leetcode-solver)](LICENSE)

`leetcode-solver` generates, executes, repairs, verifies, and publishes detailed LeetCode solutions in Python and Go. All surrounding tooling is Go. Its repository goal is complete coverage with no unverified publication: every available problem is in scope, but a solution is publishable only after the exact stored code passes every test in its pinned offline suite. An optional LeetCode submission adds a second, online gate.

The solver combines the strongest ideas from `taocp-solver` and the older `chatgpt-tool` pipelines with execution-first code evaluation:

1. Load the statement, metadata, examples, topics, and starter code from a local SQLite or DuckDB snapshot.
2. Build a candidate-blind private reference with obligations and adversarial tests.
3. Generate three independently guided candidates by default.
4. Execute every candidate against its complete, versioned offline suite in a locked-down container.
5. Select by correctness obligations, counterexample resistance, and complexity.
6. Optionally submit the selected code to LeetCode's hidden-test judge when session credentials are present.
7. Feed offline and optional online compile, runtime, wrong-answer, and failing-case evidence into a bounded repair loop.
8. Atomically save the complete JSON audit. Publish Markdown only when the final status is Accepted.

LLM review helps choose and repair candidates. It never replaces execution.

## Install

```sh
go install github.com/tamnd/leetcode-solver/cmd/leetcode-solver@latest
```

Or build from source:

```sh
git clone https://github.com/tamnd/leetcode-solver.git
cd leetcode-solver
make check build
```

## Configure

The model endpoint must implement the OpenAI Responses API. A standard API endpoint or a compatible local bridge both work.

```sh
export LEETCODE_SOLVER_BASE_URL=https://api.openai.com/v1
export LEETCODE_SOLVER_API_KEY=your-model-key
export LEETCODE_SOLVER_MODEL=gpt-5.4

export LEETCODE_SESSION=your-session-cookie
export LEETCODE_CSRF_TOKEN=your-csrf-cookie
```

Problem discovery and statement retrieval require no API key or LeetCode login. `leetcode-solver sync` mirrors the public catalog to SQLite. LeetCode credentials are optional and used only for the additional online submission gate; they are never written to an artifact. Use your own account and follow the [LeetCode Terms of Service](https://leetcode.com/terms/).

| Variable | Default | Purpose |
| --- | --- | --- |
| `LEETCODE_SOLVER_BASE_URL` | `https://api.openai.com/v1` | Model API base URL |
| `LEETCODE_SOLVER_API_KEY` | `OPENAI_API_KEY` | Model credential |
| `LEETCODE_SOLVER_MODEL` | `gpt-5.4` | Generation and review model |
| `LEETCODE_SOLVER_LANGUAGE` | `auto` | Generates both Python and Go when both starters exist |
| `LEETCODE_SOLVER_DATABASE` | `~/data/leetcode/leetcode.sqlite` | CGO-free local problem snapshot |
| `LEETCODE_SOLVER_EVAL_ROOT` | `~/data/leetcode-evals` | Versioned offline test bundles |
| `LEETCODE_SOLVER_CONTAINER_RUNTIME` | `auto` | Docker or Podman, detected locally |
| `LEETCODE_SOLVER_OUTPUT` | `~/data/leetcode-solver` | Audit and article directory |
| `LEETCODE_SOLVER_CANDIDATES` | `3` | Independent candidates, from 1 to 5 |
| `LEETCODE_SOLVER_MAX_REPAIRS` | `2` | Execution-guided repair attempts |

## Solve

```sh
leetcode-solver sync
leetcode-solver eval-sync
leetcode-solver solve two-sum
leetcode-solver solve --language golang --candidates 5 regular-expression-matching
```

On success the command prints the verified test count, optional online submission ID, and Markdown path. A solve produces:

```text
~/data/leetcode-solver/two-sum/python3.json
~/data/leetcode-solver/two-sum/python3.md
```

The JSON record retains every model response, offline suite revision and test count, optional hidden-test result, repair, timestamp, and exact final code. Failed attempts retain JSON evidence but never create or overwrite a publishable Markdown article. When Python and Go both pass, `solution.md` combines both exact implementations in the detailed `brain` article format.

## Complete catalog

Refresh the credential-free SQLite snapshot, then export a point-in-time manifest:

```sh
leetcode-solver sync
leetcode-solver catalog --json catalog.json
leetcode-solver coverage
```

Run the resumable coverage loop over every free problem:

```sh
leetcode-solver batch --parallel 1
```

Add `--include-paid` when the local snapshot contains paid statements. Accepted artifacts are cached, so interrupted runs resume without spending calls on completed work. `--language auto` requires both Python and Go for ordinary algorithm problems and chooses the available platform language for specialized SQL or shell tasks. The batch exits nonzero if even one selected implementation is missing or unaccepted.

## Writing contract

Every published article includes problem understanding, baseline and optimal approaches, an approach comparison, a stepwise algorithm, a correctness argument, exact accepted code, worked examples, complexity analysis, concrete test cases, and edge cases. Publication guards reject missing sections, empty code, process leakage, and malformed response boundaries.

The code fence in the article and the executed candidate body come from the same parsed response. A bundle may add only recorded platform scaffolding, such as Go's `package main`. The code readers see is therefore the code the suite verifies.

## Evaluation

The `eval` command consumes execution results as JSON Lines and computes the unbiased pass@k estimator:

```json
{"task_id":"livecodebench/1","dataset":"livecodebench-v2","release_date":"2025-01-01","results":[true,false,true]}
```

```sh
leetcode-solver eval --input results.jsonl
```

The benchmark design and recommended suites are documented in [docs/EVALUATION.md](docs/EVALUATION.md), and the executable format is in [docs/EVAL_BUNDLES.md](docs/EVAL_BUNDLES.md). `eval-sync` imports the pinned LeetCodeDataset Python suites entirely through Go tooling. The verified revision reads 2,869 rows and produces 2,835 runnable bundles with 274,914 assertions while reporting 34 unusable rows as gaps. The primary longitudinal regression suite is recent LiveCodeBench; EvalPlus, BigCodeBench, CodeContests, ICPC-Eval, MultiPL-E, and TestEval probe complementary correctness, performance, repair, language, and test-generation failures.

No public source contains LeetCode's proprietary hidden cases for every current problem. The coverage command reports exactly which public suite and revision backs each language implementation, its declared test count, and every gap. The project never mislabels generated or mirrored cases as LeetCode's complete hidden suite.

## Development

```sh
make fmt
make vet
make test
make lint
make check
```

Tests use scripted clients and local HTTP servers. They require no credentials and make no network calls.

## Security

Generated programs are untrusted. Offline evaluation runs with no network in a read-only, resource-limited Docker or Podman container; it never executes candidate code directly on the host. See [SECURITY.md](SECURITY.md) for credential and vulnerability guidance.

## License

MIT
