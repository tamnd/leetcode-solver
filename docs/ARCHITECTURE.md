# Architecture

```text
live catalog -> problem snapshot -> private reference
                                 -> independent candidates -> public execution
                                                            -> selection
                                                            -> offline Python/Go suites
                                                            -> optional hidden submission
                                                            -> bounded repair
                                                            -> dual-judge article gate
                                                            -> bounded correction, code held fixed
                                                            -> JSON audit
                                                            -> Markdown, only if accepted and verified
```

## Trust boundaries

The source client retrieves a statement and starter code. The model endpoint receives that public problem data and returns untrusted text. The parser extracts code and explanation through explicit tags, proves that a fenced implementation exactly matches the executable candidate, and enforces structural publication guards. The pinned offline suite is the mandatory publication gate; an optional LeetCode Accepted result is stronger independent online evidence. The filesystem store writes atomically and never persists session cookies, CSRF tokens, or model API keys.

Which credential a run spends is a boundary of its own. The `api` provider carries a key the operator supplied for that purpose. The `codex` provider instead reads an OAuth credential the Codex CLI wrote for a person's ChatGPT plan, which makes the file more sensitive than an API key: it is a login, it is shared with another program, and it is refreshed rather than static. So the solver treats it as borrowed. It reads the file, decodes the token only far enough to learn the plan and the expiry, and rewrites it solely to store a refreshed token — atomically, preserving unknown fields, keeping a backup, and holding a cooperative lock so an interleaved Codex CLI refresh cannot lose a token. A refresh that fails while the current token is still valid is logged once and the run continues, because a network blip must not invalidate a working session. The credential never enters a prompt, a log line, a benchmark row, or an artifact, and no copy of it is made anywhere else.

The bridge extends that boundary across a socket, so it is drawn at the listener. It binds loopback by default, refuses a non-loopback address without `--api-key`, and warns when an unkeyed local listener could let any process on the machine spend the plan's quota. It reads the credential before binding, so a bad credential fails as a message rather than as a listener that answers every request with the same upstream error.

The optional `leetcode-complete` snapshot has two storage boundaries. Problem statements, metadata, and starter snippets populate the ordinary `questions` table. Dataset-provided model solutions populate `reference_solutions` and are reachable only through the explicit `reference` command; the solver repository interface never reads that table. Raw split files remain in a revision-addressed, checksum-verified local cache so subsequent imports can run with networking prohibited.

## Why a private reference

The reference is derived before any candidate is visible. It lists algorithmic obligations, likely bugs, and adversarial cases, reducing anchoring on a plausible candidate. It remains fallible, so the selector is instructed to accept valid alternatives and hidden execution remains authoritative.

## Why a population

Independent candidates make correlated implementation mistakes less likely and allow execution evidence to remove compile and sample failures early. The selector compares obligations and complexity rather than prose style. Candidate count is bounded at five to control cost.

## Why remote execution

Directly running generated programs on a developer machine is unsafe. The solver executes pinned Python and Go bundles in networkless, read-only, resource-limited containers. Optional official submission remains available as independent online evidence.

Isolation alone does not make the container's report trustworthy. Candidate code shares a process with the harness, so the harness must prove what it did: it prints a completion marker carrying a nonce the runner substituted into it moments earlier, along with the number of assertions it executed against the candidate, and the runner believes a pass only when both check out. Candidates that could end the process early, or that contain the marker themselves, never reach the container. See [EVAL_BUNDLES.md](EVAL_BUNDLES.md) for the protocol and its documented residual limit.

## Why two judges

Execution decides whether the code is correct; nothing else can. But an article can be structurally complete, leak-free, and still wrong: an invariant asserted rather than proved, a complexity claim the code contradicts, a worked example that does not trace what the code would produce. One judge sees the private reference and checks those obligations by name. A second never sees the reference and tries to falsify the prose, ordered from the most likely defect to the least, and may report only what it can demonstrate — an attack it cannot show is not a defect. Requiring both to state an explicit verdict, and treating an unstated verdict as a failure, keeps a silent or evasive response from reading as approval. Neither judge can make incorrect code publishable; they can only withhold publication from correct code whose explanation does not earn it.

Corrections rewrite prose only. The code is already proved by execution, so the rewrite prompt requires it back byte for byte, and a rewrite that changed it is re-executed and discarded if it no longer passes.

## Artifact lifecycle

Each solve first writes a JSON audit. A solve that fails execution, and a solve whose code passed but whose article failed a judge, both leave a JSON record and no Markdown. Once accepted and verified, the store writes the JSON and article through temporary files followed by atomic rename. A cached candidate must pass the current offline bundle again before reuse and must itself be both accepted and verified, so an unverified record is retried rather than promoted; when online submission is requested, it must also retain or obtain a LeetCode Accepted result.
