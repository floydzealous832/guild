// Command sqlcheck is a standalone go vet analyzer that flags SQL injection
// hazards: calls to *sql.DB / *sql.Tx / *sql.Conn Query/Exec/Prepare methods
// whose first SQL string argument is built via fmt.Sprintf or string
// concatenation.
//
// Usage:
//
//	go build -o sqlcheck ./cmd/sqlcheck
//	go vet -vettool=./sqlcheck ./...
//
// Or run as a standalone binary:
//
//	./sqlcheck ./...
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(Analyzer)
}
