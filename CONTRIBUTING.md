# Contributing

Run `make check` before opening a pull request. Add deterministic tests for changes to parsing, orchestration, judge payloads, caching, or evaluation math. Unit tests must not require network access or credentials.

Keep execution as the correctness authority. A change must not publish an unaccepted solution, silently omit a selected catalog problem, log secrets, run generated code on the host, or convert infrastructure errors into passes.
