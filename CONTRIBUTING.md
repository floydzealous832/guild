# Contributing to guild

Thanks for being here. guild is a small, opinionated project — the
contributor bar is "does this keep the guild agent-agnostic,
local-first, and disciplined." If you're unsure whether something fits,
open a Discussion before writing code.

## Before you start

- Read [`AGENTS.md`](./AGENTS.md) (repo contributor conventions) and
  [`README.md`](./README.md) (end-user perspective).
- Skim [`internal/mcp/instructions.go`](./internal/mcp/instructions.go) —
  this is the full operating contract the MCP server ships to clients.

## Development setup

```bash
git clone https://github.com/mathomhaus/guild.git
cd guild
make check        # fmt + vet + lint + sqlcheck + test-race — the gate
```

Common commands:

```bash
make help              # list every make target
make check             # the full pre-commit gate
make test-race         # just the race-enabled tests
make install           # build and install ./cmd/guild to $GOBIN
make release-snapshot  # goreleaser dry-run (no publish)
```

Go 1.25+ is required. No CGO — SQLite is provided by the pure-Go
`modernc.org/sqlite`.

## Using guild while working on guild

Dogfooding is the point. Run `guild mcp install` once, then let your
agent pick up quests from the live board:

```
mcp__guild__guild_session_start(project="guild")
```

The server's `INSTRUCTIONS` string enforces the tool discipline (always
`lore_appraise` before researching, narrate after mutations, pick the
right kind for each inscribe). Follow it.

## Commit style

- Short, imperative subject lines. Prefix with the usual conventional
  verbs when useful: `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`,
  `test:`.
- Scope the message to behavior, not implementation. "why," not "what."
- If the change closes a quest on the internal board, append
  `(QUEST-N)` to the subject.
- **No AI attribution.** Do not add `Co-Authored-By: Claude` trailers,
  `🤖 Generated with ...` lines, or AI-authored comments. Tools are
  not co-authors.

## Pull requests

- One logical change per PR.
- `make check` must pass locally before you open the PR. CI runs the
  same gate.
- Update or add tests for behavior changes. A test that only re-states
  the implementation is not a test — exercise the user-visible contract.
- Update `CHANGELOG.md` under `## [Unreleased]` if your change is
  user-visible.

## Adding dependencies

Don't, unless essential. Every new module is a supply-chain surface and
a binary-size hit. Prefer stdlib, then a minimal in-tree implementation,
then — last — a well-audited third-party package.

## Reporting issues

Use the GitHub issue forms. For security, use
[GitHub Security Advisories](./SECURITY.md) — not public issues.
