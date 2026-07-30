# Goal: every problem, no missing, no wrong

The repository's north star is an accepted, rigorous, readable solution for every LeetCode problem available to the configured account.

This is an operational goal, not an unsupported claim that the current artifact set is already complete. Progress is measured against a timestamped live catalog, and correctness is measured by execution.

## Non-negotiable invariants

- Every catalog item selected by a run ends in one of two explicit states: accepted or failed. It is never silently skipped.
- Paid problems are part of the goal. A run without paid access reports them outside its selected denominator unless `--include-paid` is requested.
- A Markdown article exists only if the exact Python and Go candidate bodies pass every test in their pinned offline bundles, and only if two independent judges verified the article that accompanies them. Optional LeetCode acceptance is recorded separately.
- A pass is believed only when the harness proves it: an authenticated completion marker carrying this run's nonce, and at least as many assertions executed against the candidate as the bundle declares. A zero exit status is not evidence.
- Cached work is reused only when its audit says both Accepted and Verified for the same problem slug and language.
- Sample passage, candidate consensus, an LLM verdict, or polished prose can never establish correctness. A judge may only withhold publication; it may never confer correctness.
- Batch success means accepted equals selected. Any missing starter code, source failure, model failure, judge failure, or unaccepted solution makes the run fail.
- Each accepted artifact remains reproducible through its problem snapshot, code, model responses, judge evidence, and timestamps.

## Quality gates

1. Catalog coverage: compare accepted artifact keys with a freshly fetched catalog.
2. Compile and public examples: run before selection.
3. Offline correctness: require every case in the pinned suite to pass in the sandbox, and require the suite to prove it executed them. CI keeps a wrong candidate and a marker-forging candidate in the loop so the gate is known to refuse as well as accept.
4. Online corroboration: record LeetCode Accepted when credentials are configured.
5. Explanation fidelity: publish the article paired with the accepted code only, and only after a reference-grounded criteria judge and a reference-blind falsification judge both return an explicit pass.
6. Regression: rerun a stratified sample and recent contest slice after prompt or engine changes.
7. Benchmark: report pass@1 and pass@k on dated, execution-based suites; never tune on their hidden tests.

## Definition of done

The goal is complete for a catalog snapshot only when every free and authorized paid problem has accepted Python and Go artifacts where those languages are supported, specialized-language problems are explicitly accounted for, the public-suite coverage report has no gaps, the latest regression is green, and no article points to code different from the executed candidate.
