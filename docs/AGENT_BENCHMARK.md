# Isolated agent benchmark

The agent benchmark answers one narrow question: given the same recent LeetCode statement, starter file, model, tools, time limit, and invisible execution tests, which coding harness produces a correct `solution.py` at pass@1 and what resources did it use?

## Reproducibility pins

The Go runner refuses unverified dataset bytes and prepares a clean lab tree from exact git objects already present in the local repositories. Pins live beside the implementation in `agentbench/dataset.go` and are repeated in every generated manifest and report:

- LiveCodeBench `code_generation_lite` v6 revision and SHA-256;
- the merged tomo revision used by its container image;
- the merged tomo-labs revision providing isolation, adapters, proxy, bridge, grader, and metrics.

The default selector scans the entire v6 split, keeps functional LeetCode rows with reasonably sized private suites, and chooses the newest row in each of easy, medium, and hard. It does not read solutions from `leetcode-complete`, and neither public nor private expected outputs enter the model prompt.

## Threat model and fairness

For every cell, tomo-labs creates a fresh work directory and agent container. `/scenario` is read-only, `/work` contains only `solution.py`, and the internal container network has no route to the internet. A separate proxy is the only bridge to the configured model. The hidden oracle is a sibling of `tasks/`; it is never mounted into the agent container. Grading happens after the container exits with LiveCodeBench's vendored runner.

All tools receive the exact same user prompt, starter file, one capability attempt, and preinstalled language toolchain. Tool-specific system prompts and orchestration are intentionally preserved because those are the harnesses being compared. Codex uses native Responses passthrough on Luna so its server-injected tools remain available; the other tools are translated through the bridge on their native chat, Responses, or Anthropic wire.

## Run

Prerequisites are Go, Podman or Docker, local checkouts of `leetcode-solver` and `tomo-labs` as siblings, `OPENCODE_API_KEY` for DeepSeek, and a signed-in `~/.codex/auth.json` for Luna.

```sh
# Fetch, checksum, select, and materialize without model calls.
go run ./cmd/leetcode-solver agent-bench --prepare-only

# Prove that preparation works without network after the first sync.
go run ./cmd/leetcode-solver agent-bench --prepare-only --offline

# Small smoke before the complete matrix.
go run ./cmd/leetcode-solver agent-bench \
  --skip-build --providers deepseek \
  --tools leetcode-solver --scenarios leetcode-3773

# Six harnesses × three difficulties × two model routes.
go run ./cmd/leetcode-solver agent-bench
```

`--workspace`, `--cache`, and `--data` make every location explicit. `--tools`, `--providers`, and `--scenarios` accept comma-separated subsets. `--skip-build` reuses images only after a successful build using the same pins.

## Report contract

The runner writes `benchmark.json` for machines and `benchmark.md` for humans. Each cell includes the model route, scenario, harness, hidden-test verdict, model request count, wall time, peak RSS, and detailed token accounting. Reasoning tokens are a subset of output tokens and are not added to totals a second time. Fresh input is input minus cache reads. Cache writes are reported separately.

`list_cost_usd` applies one embedded price table uniformly to all harnesses. It is a reference estimate, not the amount charged: the DeepSeek route may be a free tier and Luna uses an existing ChatGPT subscription. Missing provider detail remains zero/unknown in the raw result rather than being invented.

These three tasks measure a recent slice, not the repository's all-problem goal. LiveCodeBench v6 ends in April 2025, so reports must publish the exact dates and must not claim that the slice is post-training for a model whose documented cutoff is later. No public corpus is identical to LeetCode's proprietary hidden suite for every problem.
