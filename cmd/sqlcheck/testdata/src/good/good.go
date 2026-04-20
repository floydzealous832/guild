// Package good contains SQL call patterns that sqlcheck MUST NOT flag.
// No want comments here — analysistest expects zero diagnostics.
package good

import (
	"context"
	"database/sql"
	"fmt"
)

var db *sql.DB
var tx *sql.Tx
var conn *sql.Conn
var ctx = context.Background()

// Case 1: Constant SQL string with parameterized placeholder — safe.
const qSelect = "SELECT id FROM t WHERE name=?"

func case1(name string) {
	_, _ = db.QueryContext(ctx, qSelect, name)
}

// Case 2: Pure bare string literal with ? placeholders — safe.
func case2(x, id int) {
	_, _ = db.Exec("UPDATE t SET x=? WHERE id=?", x, id)
}

// Case 3: fmt.Sprintf called for non-SQL purposes — must not flag.
func case3(name string) {
	msg := fmt.Sprintf("hello %s", name)
	_ = msg // not passed to any SQL function
}

// Case 4: Const ident passed to QueryContext — safe (constant traced).
const qDelete = "DELETE FROM t WHERE id=?"

func case4(id int) {
	_, _ = db.ExecContext(ctx, qDelete, id)
}

// Case 5: Pure literal in Query — safe.
func case5() {
	_, _ = db.Query("SELECT 1")
}

// Case 6: Tx with parameterized literal — safe.
func case6(project string) {
	_, _ = tx.QueryContext(ctx, "SELECT id FROM entries WHERE project=?", project)
}

// Case 7: Conn with parameterized Prepare — safe.
func case7() {
	_, _ = conn.PrepareContext(ctx, "INSERT INTO t (col) VALUES (?)")
}

// Case 8: fmt.Sprintf result passed to a non-SQL function — safe, no flag.
func logMsg(s string) {}

func case8(v string) {
	logMsg(fmt.Sprintf("value: %s", v))
}

// Case 9: Two string constants concatenated — both sides const, safe.
const prefix = "SELECT id FROM t WHERE "
const suffix = "status=?"

func case9() {
	_, _ = db.Query(prefix + suffix)
}

// Case 10: QueryRow with a pure literal — safe.
func case10(id int) {
	_ = db.QueryRow("SELECT name FROM t WHERE id=?", id)
}

// Case 11: Prepare with a plain literal — safe.
func case11() {
	_, _ = db.Prepare("SELECT * FROM t WHERE id=?")
}
