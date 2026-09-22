package platform

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrorClass names the kind of error err is, in words that carry nothing of the
// error's own text: a context's end, a connection's deadline, reset or closed
// pipe, a Postgres SQLSTATE, a lost database connection, a cut read, or - for
// anything it does not recognise - the Go type of the innermost error, which is
// a name in this program's source and never data. "none" for nil.
//
// IT EXISTS FOR LOG LINES THAT ARE KEPT. The text of a failed write names the
// client's address and port, and an error wrapped further up could carry
// whatever its wrapper put in it, such as a buyer's email address. The Holder
// Export's "finished" audit line records an abort by this class (ADR 0075).
func ErrorClass(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "write_deadline_exceeded"
	case errors.Is(err, syscall.EPIPE):
		return "broken_pipe"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.As(err, &pgErr):
		return "postgres " + pgErr.Code
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, sql.ErrConnDone):
		return "database_connection_lost"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "unexpected_eof"
	}
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return fmt.Sprintf("%T", err)
		}
		err = inner
	}
}
