# Goal: every problem, no missing, no wrong

The repository's north star is an accepted, rigorous, readable solution for every LeetCode problem available to the configured account.

This is an operational goal, not an unsupported claim that the current artifact set is already complete. Progress is measured against a timestamped live catalog, and correctness is measured by execution.

## Non-negotiable invariants

- Every catalog item selected by a run ends in one of two explicit states: accepted or failed. It is never silently skipped.
- Paid problems are part of the goal. A run without paid access reports them outside its selected denominator unless `--include-paid` is requested.
- A Markdown article exists only if the exact Python and Go candidate bodies pass every test in their pinned offline bundles. Optional LeetCode acceptance is recorded separately.
- Cached work is reused only when its audit says Accepted for the same problem slug and language.
- Sample passage, candidate consensus, an LLM verdict, or polished prose can never establish correctness.
- Batch success means accepted equals selected. Any missing starter code, source failure, model failure, judge failure, or unaccepted solution makes the run fail.
- Each accepted artifact remains reproducible through its problem snapshot, code, model responses, judge evidence, and timestamps.

## Quality gates

1. Catalog coverage: compare accepted artifact keys with a freshly fetched catalog.
2. Compile and public examples: run before selection.
3. Offline correctness: require every case in the pinned suite to pass in the sandbox.
4. Online corroboration: record LeetCode Accepted when credentials are configured.
5. Explanation fidelity: publish the article paired with the accepted code only.
6. Regression: rerun a stratified sample and recent contest slice after prompt or engine changes.
7. Benchmark: report pass@1 and pass@k on dated, execution-based suites; never tune on their hidden tests.

## Definition of done

The goal is complete for a catalog snapshot only when every free and authorized paid problem has accepted Python and Go artifacts where those languages are supported, specialized-language problems are explicitly accounted for, the public-suite coverage report has no gaps, the latest regression is green, and no article points to code different from the executed candidate.
