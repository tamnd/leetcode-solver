# Offline evaluation bundles

Every publishable language implementation requires a local bundle at:

```text
$LEETCODE_SOLVER_EVAL_ROOT/<problem-slug>/<language>/manifest.json
```

The bundle contains tests converted from a pinned public evaluation dataset or independently generated and reviewed tests. Candidate code is not stored in the suite. At runtime it is copied into an ephemeral directory beside the harness and mounted read-only into a container.

Example Python manifest:

```json
{
  "schema_version": 1,
  "problem_slug": "two-sum",
  "language": "python3",
  "dataset": "leetcode-evalplus",
  "revision": "git:0123456789abcdef",
  "image": "python:3.13-alpine@sha256:<digest>",
  "candidate_file": "solution.py",
  "command": ["python3", "-I", "test_solution.py"],
  "files": ["test_solution.py"],
  "test_count": 250,
  "timeout_seconds": 30
}
```

Example Go manifest:

```json
{
  "schema_version": 1,
  "problem_slug": "two-sum",
  "language": "golang",
  "dataset": "multipl-e-plus",
  "revision": "git:fedcba9876543210",
  "image": "golang:1.26-alpine@sha256:<digest>",
  "candidate_file": "solution.go",
  "candidate_prefix": "package main\n\n",
  "command": ["go", "test", "-count=1", "solution.go", "solution_test.go"],
  "files": ["solution_test.go"],
  "test_count": 250,
  "timeout_seconds": 30
}
```

The runner requires a digest-pinned image already present locally and passes `--pull=never`, so evaluation itself is 100% offline. Containers have no network, a read-only root filesystem and workspace, dropped Linux capabilities, `no-new-privileges`, PID, memory, CPU, output, and wall-time limits, and an ephemeral `/tmp`.

`candidate_prefix` and `candidate_suffix` contain only platform scaffolding. For example, LeetCode Go submissions omit a package declaration, while `go test` requires one. The candidate body between those fields remains byte-for-byte identical to the code in the artifact and article.

A bundle is invalid when its identity does not match the problem and language, its dataset revision is absent, it declares zero tests, a path escapes the bundle, or its image or command is missing. Any invalid or missing bundle blocks publication.

Converters are implemented in Go and preserve upstream task IDs and revisions. The built-in `eval-sync` command imports the pinned LeetCodeDataset Python corpus. Python coverage should expand with EvalPlus and LiveCodeBench-derived LeetCode tasks. Go coverage should begin with MultiPL-E and equivalent translated property tests. Differential fuzz cases may extend those suites, but must use a separately reviewed oracle and record their seed.
