# Evaluation strategy

Evaluation separates production acceptance from research measurement. Production correctness requires every test in the pinned offline bundle to pass for the exact candidate body. LeetCode Accepted is an optional, stronger online corroboration. Benchmarks compare models, prompts, candidate counts, and repair policies on frozen datasets without changing the offline gate.

## Primary suite

[LiveCodeBench](https://github.com/LiveCodeBench/LiveCodeBench) is the primary longitudinal suite because it continuously collects contest problems from LeetCode, AtCoder, and Codeforces, supports code generation and self-repair, records release dates, and evaluates by execution. Use its full code-generation suite for releases and its lite suite for pull requests. Report a post-training-cutoff date slice separately to reduce contamination.

## Complementary suites

| Suite | What it catches | Recommended use |
| --- | --- | --- |
| [EvalPlus](https://github.com/evalplus/evalplus) | Fragile function solutions missed by small public suites; HumanEval+ and MBPP+ add many tests | Fast correctness regression and performance checks with EvalPerf |
| [BigCodeBench](https://github.com/bigcode-project/bigcodebench) | Complex instructions, multiple library calls, and broader software tasks | Generalization beyond interview algorithms |
| [CodeContests](https://github.com/google-deepmind/code_contests) | Competitive-programming problems with an official execution harness | Large-scale algorithmic regression |
| [ICPC-Eval](https://github.com/RUCAIBox/Slow_Thinking_with_LLMs) | Difficult recent contests and feedback-driven refinement | Measure repair quality and Refine@k |
| [MultiPL-E](https://github.com/nuprl/MultiPL-E) | The same tasks translated across many languages | Detect language-specific prompt and runtime failures |
| [TestEval](https://github.com/LLM4SoftwareTesting/TestEval) | Test generation on 210 LeetCode-derived Python programs | Evaluate adversarial-test quality, branch, and path coverage |
| [LiveBench](https://github.com/LiveBench/LiveBench) | Objective, regularly refreshed coding questions | Independent contamination-resistant check |
| [LeetCodeDataset](https://github.com/newfacade/LeetCodeDataset) | Roughly 2.9K Python problems with generated execution tests and temporal metadata | Main direct LeetCode import; pin the dataset commit |

HumanEval or MBPP alone is not a release gate because their original tests are too small. An LLM judge score is diagnostic only and must not be mixed into execution pass rates.

## Public coverage limit

Public repositories and Hugging Face datasets provide broad but incomplete coverage. The verified pinned LeetCodeDataset v0.3.1 import read 2,869 rows and produced 2,835 runnable Python bundles with 274,914 assertions; 34 rows lacked a usable test or entry point. The live LeetCode catalog is larger and keeps changing. These generated cases are not a release of LeetCode's proprietary hidden tests. Other useful mirrors add examples or community tests but likewise do not establish complete hidden-suite coverage. Therefore, “100% offline” means that fetching is completed ahead of time and every locally declared test runs without network access. It does not mean that a public corpus is identical to every private LeetCode case.

## Experimental protocol

1. Freeze the exact dataset revision, date window, model identifier, endpoint, prompt revision, candidate count, repair budget, timeout, and language.
2. Generate `n` independent samples per task. Keep failures and timeouts in the denominator.
3. Execute in the benchmark's maintained sandbox. Never run generated code on the CI host without isolation.
4. Export one JSONL row per task with the boolean result of each sample.
5. Report task count, sample count, raw accepted count, pass@1, pass@5, and pass@10 where `n` permits.
6. Slice results by date, difficulty, topic, language, failure class, candidate-selection outcome, and repair count.
7. Compare changes on identical samples where possible. Report uncertainty and timeout sensitivity for close results.

`leetcode-solver eval` uses the standard unbiased estimator

$$
\operatorname{pass@k}=1-\frac{\binom{n-c}{k}}{\binom{n}{k}},
$$

where $n$ is the number of generated samples and $c$ is the number that pass execution.

## Production coverage metrics

Benchmark pass@k does not measure repository completeness. A coverage report must separately record the catalog timestamp, total problems, free problems, authorized paid problems, accepted artifacts, failures by phase, and missing artifact keys. The batch command deliberately exits nonzero when accepted is below selected.

## Leakage controls

- Prefer problems released after the evaluated model's documented training cutoff.
- Do not include official solutions, accepted community code, or hidden benchmark tests in generation prompts.
- Keep private tests inaccessible to the model and repair only from ordinary execution feedback.
- Version prompts and benchmark conversions with results.
- Never compare a tuned run with an earlier untuned run on a different dataset revision.
