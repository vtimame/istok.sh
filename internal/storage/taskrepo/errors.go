package taskrepo

import (
	"errors"

	"github.com/mattn/go-sqlite3"

	"s26.dev/istok-cli/internal/task"
)

func mapSQLError(err error) error {
	var value sqlite3.Error
	if errors.As(err, &value) && (value.Code == sqlite3.ErrConstraint || value.ExtendedCode == sqlite3.ErrConstraintPrimaryKey || value.ExtendedCode == sqlite3.ErrConstraintUnique) {
		return task.NewError(task.CodeConflict, "task conflicts with existing data")
	}

	return err
}
