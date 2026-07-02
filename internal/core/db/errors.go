package db

import (
	"errors"

	sqlite "modernc.org/sqlite"
)

// SQLite extended result codes for constraint violations.
// See https://www.sqlite.org/rescode.html.
const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// IsUniqueViolation reports whether err is a UNIQUE or PRIMARY KEY constraint
// failure. This is the one place driver-specific error inspection lives, so
// callers stay decoupled from the SQLite driver.
func IsUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code()
		return code == sqliteConstraintUnique || code == sqliteConstraintPrimaryKey
	}
	return false
}
