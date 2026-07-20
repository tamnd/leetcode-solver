# Architecture

```text
live catalog -> problem snapshot -> private reference
                                 -> independent candidates -> public execution
                                                            -> selection
                                                            -> offline Python/Go suites
                                                            -> optional hidden submission
                                                            -> bounded repair
                                                            -> JSON audit
                                                            -> Markdown, only if Accepted
```

## Trust boundaries

The source client retrieves a statement and starter code. The model endpoint receives that public problem data and returns untrusted text. The parser extracts code and explanation through explicit tags, proves that a fenced implementation exactly matches the executable candidate, and enforces structural publication guards. The pinned offline suite is the mandatory publication gate; an optional LeetCode Accepted result is stronger independent online evidence. The filesystem store writes atomically and never persists session cookies, CSRF tokens, or model API keys.

The optional `leetcode-complete` snapshot has two storage boundaries. Problem statements, metadata, and starter snippets populate the ordinary `questions` table. Dataset-provided model solutions populate `reference_solutions` and are reachable only through the explicit `reference` command; the solver repository interface never reads that table. Raw split files remain in a revision-addressed, checksum-verified local cache so subsequent imports can run with networking prohibited.

## Why a private reference

The reference is derived before any candidate is visible. It lists algorithmic obligations, likely bugs, and adversarial cases, reducing anchoring on a plausible candidate. It remains fallible, so the selector is instructed to accept valid alternatives and hidden execution remains authoritative.

## Why a population

Independent candidates make correlated implementation mistakes less likely and allow execution evidence to remove compile and sample failures early. The selector compares obligations and complexity rather than prose style. Candidate count is bounded at five to control cost.

## Why remote execution

Directly running generated programs on a developer machine is unsafe. The solver executes pinned Python and Go bundles in networkless, read-only, resource-limited containers. Optional official submission remains available as independent online evidence.

## Artifact lifecycle

Each solve first writes a JSON audit. An unaccepted solve has no Markdown output. Once Accepted, the store writes the JSON and article through temporary files followed by atomic rename. A cached candidate must pass the current offline bundle again before reuse; when online submission is requested, it must also retain or obtain a LeetCode Accepted result.
