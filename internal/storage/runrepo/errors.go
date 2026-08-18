package runrepo

import (
	"errors"

	"github.com/mattn/go-sqlite3"

	runmodel "github.com/vtimame/istok.sh/internal/run"
)

func mapSQLError(err error, message string) error {
	var sqliteError sqlite3.Error
	if errors.As(err, &sqliteError) && sqliteError.Code == sqlite3.ErrConstraint {
		return runmodel.NewError(runmodel.CodeConflict, "%s", message)
	}

	return err
}
