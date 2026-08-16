package contextrepo

import (
	"errors"

	"github.com/mattn/go-sqlite3"

	contextmodel "s26.dev/istok-cli/internal/context"
)

func mapSQLError(err error) error {
	var value sqlite3.Error
	if errors.As(err, &value) && (value.Code == sqlite3.ErrConstraint || value.ExtendedCode == sqlite3.ErrConstraintPrimaryKey || value.ExtendedCode == sqlite3.ErrConstraintUnique) {
		return contextmodel.NewError(contextmodel.CodeConflict, "context record conflicts with existing data")
	}

	return err
}
