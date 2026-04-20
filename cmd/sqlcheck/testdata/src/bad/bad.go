// Package bad contains SQL call patterns that sqlcheck MUST flag.
// Each flagged call is annotated with a want comment that analysistest
// matches against the diagnostic message.
package bad

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// Helpers so the file compiles without a real DB.
var db *sql.DB
var tx *sql.Tx
var conn *sql.Conn
var ctx = context.Background()

// Case 1: fmt.Sprintf directly in QueryContext arg.
func case1(name string) {
	_, _ = db.QueryContext(ctx, fmt.Sprintf("SELECT id FROM t WHERE name='%s'", name)) // want `SQL query built via fmt\.Sprintf`
}

// Case 2: string concatenation in Exec arg.
func case2(tableName string) {
	_, _ = db.Exec("DELETE FROM " + tableName + " WHERE id=1") // want `SQL query built via string concatenation`
}

// Case 3: concatenation via strconv in a local var, traced by the analyzer.
func case3(id int) {
	q := "SELECT x FROM t WHERE id=" + strconv.Itoa(id) // tainted
	_, _ = db.Query(q)                                  // want `SQL query built via string concatenation`
}

// Case 4: fmt.Sprintf in QueryRow.
func case4(status string) {
	_ = db.QueryRow(fmt.Sprintf("SELECT count(*) FROM t WHERE status='%s'", status)) // want `SQL query built via fmt\.Sprintf`
}

// Case 5: fmt.Sprintf in Prepare.
func case5(col string) {
	_, _ = db.Prepare(fmt.Sprintf("CREATE INDEX ON t (%s)", col)) // want `SQL query built via fmt\.Sprintf`
}

// Case 6: Tx receiver — same rules apply.
func case6(name string) {
	_, _ = tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO t (name) VALUES ('%s')", name)) // want `SQL query built via fmt\.Sprintf`
}

// Case 7: Tx QueryContext with concatenation.
func case7(project string) {
	_, _ = tx.QueryContext(ctx, "SELECT id FROM entries WHERE project='"+project+"'") // want `SQL query built via string concatenation`
}

// Case 8: Conn receiver with Sprintf.
func case8(val string) {
	_, _ = conn.ExecContext(ctx, fmt.Sprintf("UPDATE t SET col='%s' WHERE id=1", val)) // want `SQL query built via fmt\.Sprintf`
}

// Case 9: local var initialized by Sprintf, then passed to Query.
func case9(x string) {
	q := fmt.Sprintf("SELECT * FROM t WHERE x='%s'", x)
	_, _ = db.Query(q) // want `SQL query built via fmt\.Sprintf`
}

// Case 10: PrepareContext with concatenation.
func case10(schema string) {
	_, _ = db.PrepareContext(ctx, "SELECT * FROM "+schema+".entries") // want `SQL query built via string concatenation`
}
