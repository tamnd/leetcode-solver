# Offline evaluation bundles

Every publishable language implementation requires a local bundle at:

```text
$LEETCODE_SOLVER_EVAL_ROOT/<problem-slug>/<language>/manifest.json
```

The bundle contains tests converted from a pinned public evaluation dataset or independently generated and reviewed tests. Candidate code is not stored in the suite. At runtime it is copied into an ephemeral directory beside the harness and mounted read-only into a container.

## What the runner requires before it believes a pass

A zero exit status is not evidence. A candidate that raises `SystemExit` while it is being imported exits zero. A harness whose assertions are never reached exits zero. `go test` prints `ok` for a package whose test wrote nothing. Schema 3 therefore requires three independent things, and treats the absence of any of them as a failure:

1. **A completion marker.** After its last assertion, the harness prints on a line of its own:

   ```text
   LEETCODE_SOLVER_PASS <nonce> assertions=<count>
   ```

2. **This run's nonce.** Before each run the runner generates a fresh 128-bit token and substitutes it for every occurrence of `{{LEETCODE_SOLVER_NONCE}}` in every declared harness file. A marker that does not carry that exact token is ignored. A bundle in which no harness file contains the placeholder is rejected outright, because nothing it prints could be authenticated.

3. **At least as many executed assertions as the manifest declares.** `assertions_run` counts real calls into the candidate — the Python harness wraps the entry point in a counter, the Go harness increments after each comparison — so a harness that silently skipped cases cannot pass. The artifact and the success line report the executed count, not the declared one.

The candidate is also screened before the container starts. Code that calls `sys.exit`, `os._exit`, `os.abort`, `exit()`, `quit()`, `os.Exit`, `syscall.Exit`, or `runtime.Goexit`, or that contains the marker token itself, is refused: such a candidate can decide its own verdict, and no LeetCode solution body needs any of them.

**Known limit.** Candidate code shares a process with the harness, so a candidate that deliberately reads the harness source beside it can read the nonce. The nonce defeats the realistic failure — a hardcoded success line printed before any assertion runs — not a determined adversary. Closing that gap requires a two-process protocol in which the expected values reach a parent over a pipe and never enter the candidate's process.

CI proves the gate refuses as well as accepts: `testdata/solutions/two-sum-wrong.go.txt` and `testdata/solutions/two-sum-forged-marker.py` must both be rejected on every run.

## Manifests

Example Python manifest:

```json
{
  "schema_version": 3,
  "problem_slug": "two-sum",
  "language": "python3",
  "dataset": "newfacade/LeetCodeDataset",
  "revision": "hf:a9eca795817a0a21132070e2dc2e87445da4f089:v0.3.1",
  "image": "python:3.13-alpine@sha256:<digest>",
  "candidate_file": "solution.py",
  "command": ["python3", "-I", "test_solution.py"],
  "files": ["test_solution.py"],
  "test_count": 250,
  "timeout_seconds": 60
}
```

Example Go manifest:

```json
{
  "schema_version": 3,
  "problem_slug": "two-sum",
  "language": "golang",
  "dataset": "newfacade/LeetCodeDataset",
  "revision": "hf:a9eca795817a0a21132070e2dc2e87445da4f089:v0.3.1",
  "image": "golang:1.26.5-alpine@sha256:<digest>",
  "candidate_file": "solution.go",
  "candidate_prefix": "package main\n\n",
  "command": ["go", "test", "-v", "-count=1", "solution.go", "solution_test.go"],
  "files": ["solution_test.go"],
  "test_count": 250,
  "timeout_seconds": 120
}
```

`-v` is mandatory for `go test` and the manifest is rejected without it: `cmd/go` prints only `ok <package>` for a passing package and discards what the test wrote, which would hide the marker and fail every correct Go candidate.

The runner requires a digest-pinned image already present locally and passes `--pull=never`, so evaluation itself is 100% offline. Containers have no network, a read-only root filesystem and workspace, dropped Linux capabilities, `no-new-privileges`, PID, memory, CPU, output, and wall-time limits, and an ephemeral `/tmp` mounted `noexec`. Go additionally receives a bounded exec-capable `/go-tmp` for `GOCACHE` and `GOTMPDIR`, because the toolchain must run the test binary it builds.

`candidate_prefix` and `candidate_suffix` contain only platform scaffolding. For example, LeetCode Go submissions omit a package declaration, while `go test` requires one. The candidate body between those fields remains byte-for-byte identical to the code in the artifact and article.

A bundle is invalid when its schema version is not 3, its identity does not match the problem and language, its dataset revision is absent, it declares zero tests, a path escapes the bundle, its image is not digest-pinned, its command is missing, a `go test` command omits `-v`, or no harness file carries the nonce placeholder. Any invalid or missing bundle blocks publication.

## Generation

Converters are implemented in Go and preserve upstream task IDs and revisions. `eval-sync` imports the pinned LeetCodeDataset corpus and writes both languages from the same cases:

- **Python** wraps the dataset's own `check` function. The candidate is imported inside a `SystemExit` guard, and the entry point is wrapped so the harness publishes how many assertions it truly executed.
- **Go** is derived from the same rows. Each `assert candidate(...) == <literal>` is lifted out of the check function, the arguments are matched by name against the problem's real Go starter signature from the problem repository, and the values are rendered as typed Go literals. Extraction is all-or-nothing: a check function containing an assertion in any other shape yields no Go bundle at all, because replaying a subset while declaring the same authority would overstate what was proved.

A problem gets no Go bundle when its starter signature cannot be driven by a table of literals — a linked list or tree pointer, a design problem's method set, a mutate-in-place signature with no return value — or when its check function uses a tolerance comparison, a wrapped call such as `sorted(candidate(...))`, or a constructed expected value. Every such refusal is reported with its reason, and `--go-gaps <path>` writes the full list as JSON. `coverage` measures against what a complete answer owes, so these gaps count against the repository rather than shrinking its claim.

Python coverage should expand with EvalPlus and LiveCodeBench-derived LeetCode tasks. Go coverage should extend to the refused shapes with purpose-built harnesses — a linked-list and tree encoder, and a stateful driver for design problems. Differential fuzz cases may extend those suites, but must use a separately reviewed oracle and record their seed.
