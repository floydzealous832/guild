package quest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mathomhaus/guild/internal/project"
)

// Init registers the project at rootPath under name. Thin wrapper
// around project.Register — kept in this package so the CLI layer can
// call a single `quest.Init` without reaching into project/ (and so we
// have a seam to add quest-specific init behavior later, e.g. auto-
// seeding from TASKS.md).
//
// name is the project id (directory basename). rootPath must be
// absolute. tasksFile defaults to "TASKS.md" when empty.
//
// Returns the registered Project (post-upsert) for callers that want
// to echo the path in a confirmation line.
func Init(ctx context.Context, db *sql.DB, name, rootPath, tasksFile string) (*project.Project, error) {
	if db == nil {
		return nil, fmt.Errorf("quest: init: nil db")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("quest: init: empty name")
	}
	if strings.TrimSpace(rootPath) == "" {
		return nil, fmt.Errorf("quest: init: empty path")
	}
	if err := project.Register(ctx, db, name, rootPath, tasksFile); err != nil {
		return nil, err
	}
	return project.LookupByName(ctx, db, name)
}
