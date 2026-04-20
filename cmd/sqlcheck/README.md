# sqlcheck — SQL injection safety analyzer

Custom `go/analysis` analyzer that flags SQL injection hazards: calls to
`*sql.DB`, `*sql.Tx`, and `*sql.Conn` Query/Exec/Prepare methods whose first
SQL string argument is built via `fmt.Sprintf` or string concatenation (`+`).

This implements the §10b.0b rule: every SQL statement
must use `?` placeholders with driver-side binding. Tool inputs in guild MCP
are agent-controlled strings — one unguarded `fmt.Sprintf` is a CVE.

---

## Build

```sh
go build -o sqlcheck ./cmd/sqlcheck
```

---

## Run

### As a standalone binary (simplest)

```sh
./sqlcheck ./...
```

### As a go vet tool (integrates with existing vet pipeline)

```sh
go vet -vettool=./sqlcheck ./...
```

Both exit non-zero if any SQL injection pattern is found.

---

## What it flags

| Pattern | Example | Status |
|---|---|---|
| `fmt.Sprintf` in SQL arg | `db.Query(fmt.Sprintf("SELECT ... '%s'", v))` | FLAGGED |
| String concat (`+`) in SQL arg | `db.Exec("DELETE FROM " + tbl + " WHERE id=1")` | FLAGGED |
| Local var assigned from Sprintf | `q := fmt.Sprintf(...); db.Query(q)` | FLAGGED |
| Local var assigned from concat | `q := "SELECT ... " + val; db.Query(q)` | FLAGGED |
| Const SQL + placeholder params | `db.QueryContext(ctx, qConst, arg)` | ALLOWED |
| Bare string literal | `db.Exec("DELETE FROM t WHERE id=?", id)` | ALLOWED |
| Two constants concatenated | `db.Query(constA + constB)` | ALLOWED |

---

## Suppression

For cases where concatenation is intentional and safe (e.g. PRAGMA queries
in tests), suppress with a `//nolint:sqlcheck` comment on the same line:

```go
row := conn.QueryRowContext(ctx, "PRAGMA "+pragmaName) //nolint:sqlcheck
```

By default, `_test.go` files are NOT checked. To include them:

```sh
./sqlcheck -sqlcheck.include-tests ./...
```

---

## golangci-lint wiring

**golangci-lint v2 (custom-gcl):** golangci-lint v2 supports custom analyzers
via its plugin system, but requires building a shared `.so` plugin which is
OS-specific and adds CI complexity. This is deferred as a follow-up.

**Current CI gate:** Add this step to your GitHub Actions workflow after the
standard golangci-lint step:

```yaml
- name: Build sqlcheck
  run: go build -o sqlcheck ./cmd/sqlcheck

- name: Run sqlcheck
  run: go vet -vettool=./sqlcheck ./...
```

**Why not wired directly into `.golangci.yml`:** golangci-lint v1 custom linter
support requires a Go plugin (`.so` file) built with `CGO_ENABLED=1` against
the exact same Go toolchain and golangci-lint version. This is fragile on macOS
(arm64 + amd64 cross-compile) and impossible in pure-Go mode. The `go vet
-vettool` approach is the official, supported alternative and is just as
reliable as a CI gate.

---

## Detected receiver types

- `*database/sql.DB` — Query, QueryRow, QueryContext, QueryRowContext, Exec, ExecContext, Prepare, PrepareContext
- `*database/sql.Tx` — same set
- `*database/sql.Conn` — same set

---

## Scope and limitations

- Traces local variables one level deep (covers the `q := fmt.Sprintf(...); db.Query(q)` pattern)
- Does NOT trace struct field values or cross-function assignments (complexity vs. value tradeoff)
- Does NOT detect `strings.Builder` composition patterns (uncommon for SQL; document limitation)
- Test files (`_test.go`) are skipped by default — they often use PRAGMA queries or other safe controlled concatenations
