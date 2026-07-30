# Security policy

## Supported versions

Security fixes are applied to the latest release and `main`.

## Reporting

Report vulnerabilities through GitHub private vulnerability reporting. Do not open a public issue for credential exposure or an exploitable execution flaw.

## Credentials

Use environment variables or a secret manager. Never place `LEETCODE_SESSION`, `LEETCODE_CSRF_TOKEN`, or model API keys in flags in shared shell history, repository files, logs, benchmark rows, or artifacts. Revoke a cookie or key immediately if it is exposed.

## ChatGPT subscription credential

`--provider codex` and the `bridge` command read the Codex CLI's OAuth credential, by default `~/.codex/auth.json`. It is a login for a person's ChatGPT account, not a scoped API key, so treat it accordingly: keep the file readable only by its owner, do not commit or copy it, and sign out of the Codex CLI to revoke it if it leaks. The solver reads it, decodes the token only to report the plan and expiry, and writes it only to store a refreshed token — atomically, preserving unknown fields, keeping a `.bak` beside it, and holding a lock file so a concurrent Codex CLI refresh cannot lose a token. The credential is never placed in a prompt, log line, benchmark row, or artifact.

`bridge` turns that credential into an endpoint anything on the machine can call. Leave it on loopback, and give it `--api-key` whenever another user shares the host; a non-loopback bind without a key is refused.

## Generated code

Generated code is untrusted. The offline runner never invokes it on the host: it requires a preloaded, digest-pinned container image and disables network access while applying read-only filesystems, dropped capabilities, no-new-privileges, PID, memory, CPU, output, and wall-time limits. Optional LeetCode submission sends the same candidate body to the online judge.
