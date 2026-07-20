# LeetCode test-source audit

This audit distinguishes broad public coverage from the unavailable proprietary hidden suite. Dataset revisions must be pinned before import, and their licenses apply to downloaded material.

| Source | Useful coverage | Limitation | Integration |
| --- | --- | --- | --- |
| [newfacade/LeetCodeDataset](https://github.com/newfacade/LeetCodeDataset) / [Hugging Face](https://huggingface.co/datasets/newfacade/LeetCodeDataset) | The pinned v0.3.1 revision has 2,641 train and 228 test Python rows. The verified import produced 2,835 runnable bundles containing 274,914 assertions; 34 rows had no usable tests or entry point. | Python only; generated tests; does not cover the current full catalog or reproduce private hidden tests | Implemented by the Go `eval-sync` importer |
| [tkeskin/leetcode-solutions](https://huggingface.co/datasets/tkeskin/leetcode-solutions) | About 3,169 Python solutions and 3,495 C++ solutions, plus input/output data inherited from LeetCodeDataset | Test coverage is bounded by the secondary source; no Go column; solution data must never enter generation prompts | Candidate source for independent oracle comparison only |
| [LiveCodeBench](https://github.com/LiveCodeBench/LiveCodeBench) | Recent LeetCode, AtCoder, and Codeforces tasks with execution tests and dated releases | Hundreds of contest tasks, not all LeetCode problems; mostly standard-input programs rather than LeetCode method wrappers | Benchmark adapter target |
| [EvalPlus](https://github.com/evalplus/evalplus) | HumanEval+ and MBPP+ greatly expand tests and include performance evaluation | Not a LeetCode catalog mirror | Fast Python robustness regression |
| [MultiPL-E](https://github.com/nuprl/MultiPL-E) | Translates function benchmarks to Go and many other languages | Covers its benchmark tasks, not the LeetCode catalog | Primary design reference for generated Go harnesses |
| [QuBenhao/LeetCode](https://github.com/QuBenhao/LeetCode) | Local runners and shared Python, Go, C++, Java, Rust, and TypeScript adapters | Repository tests track its maintained solutions and are not a complete per-problem corpus | Go wrapper and type-adapter reference |
| [king133134/leetCodeTests](https://github.com/king133134/leetCodeTests) | Generates Go tests from LeetCode examples and claims support for over 98% of example shapes | Examples only; documented unsupported structures; no hidden cases | Example importer reference |
| [TestEval](https://github.com/LLM4SoftwareTesting/TestEval) | Test-generation and coverage tasks derived from 210 LeetCode Python programs | Measures test generation, not complete solution correctness | Adversarial-test quality benchmark |
| [BAAI/TACO](https://huggingface.co/datasets/BAAI/TACO) and [Google CodeContests](https://github.com/google-deepmind/code_contests) | Large execution-based competitive-programming corpora | Mixed platforms and standard input/output; not full LeetCode | General algorithm regression |

## Finding

No located public GitHub or Hugging Face source contains every official test case for every current LeetCode problem. LeetCode's private judge cases are not published as a complete dataset, the live catalog changes, premium content has separate access constraints, and public mirrors have language, date, problem-type, and test-generation gaps.

Consequently the repository uses three explicit terms:

- **offline-complete for an implementation**: every case declared by that implementation's pinned bundle ran and passed without network access;
- **catalog-covered**: every required problem-language key has a valid nonempty bundle;
- **online-accepted**: LeetCode separately returned Accepted for the candidate.

Only the first two can be established entirely offline. Neither is represented as proof that public cases equal LeetCode's complete hidden suite. The `coverage` command makes remaining gaps machine-visible and exits nonzero until they are resolved.
