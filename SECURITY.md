# Security policy

## Supported versions

guild is pre-1.0. Security fixes are made against the latest tagged
release on `main`. There is no long-term-support branch yet.

## Reporting a vulnerability

Please **do not** file a public GitHub issue for security problems.

Report privately via GitHub Security Advisories:
<https://github.com/mathomhaus/guild/security/advisories/new>

Include:

- a minimal reproduction (commands, inputs, expected vs. observed),
- the affected version (`guild --version`),
- your assessment of impact (local data disclosure, privilege escalation, etc.).

You'll get an initial response within a few days. Coordinated disclosure
timelines are negotiated case-by-case; the default target is a patched
release within 30 days of triage.

## Scope

guild is designed to be local-first. It reads and writes SQLite under
`~/.guild/` and speaks MCP over stdio. In-scope areas include:

- the `guild` binary and its subcommands,
- the MCP server (`guild mcp serve`) and its tool handlers,
- schema migrations and on-disk data formats,
- installer helpers (`guild mcp install`, `guild init`).

Out of scope:

- vulnerabilities in upstream MCP clients (report those to the client's project),
- issues only reproducible with hand-edited `~/.guild/*.db` files.

## Privacy

guild is local-first: no outbound network calls, no analytics, no
account. Data-handling details (what `~/.guild/usage.log` contains,
how to opt out, log rotation caps) live in the `Privacy` section of
[README.md](./README.md#-privacy).
