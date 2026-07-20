# Security policy

## Supported versions

Security fixes are applied to the latest release and `main`.

## Reporting

Report vulnerabilities through GitHub private vulnerability reporting. Do not open a public issue for credential exposure or an exploitable execution flaw.

## Credentials

Use environment variables or a secret manager. Never place `LEETCODE_SESSION`, `LEETCODE_CSRF_TOKEN`, or model API keys in flags in shared shell history, repository files, logs, benchmark rows, or artifacts. Revoke a cookie or key immediately if it is exposed.

## Generated code

Generated code is untrusted. The offline runner never invokes it on the host: it requires a preloaded, digest-pinned container image and disables network access while applying read-only filesystems, dropped capabilities, no-new-privileges, PID, memory, CPU, output, and wall-time limits. Optional LeetCode submission sends the same candidate body to the online judge.
