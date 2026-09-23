package platform

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrorClass names the kind of error err is, in words that carry nothing of the
// error's own text: a context's end, a connection's deadline, reset or closed
// pipe, a Postgres SQLSTATE, a database that cannot be reached, a lost database
// connection, a cut read, or - for anything it does not recognise - the Go type
// of the most specific error inside it, which is a name in this program's
// source and never data. "none" for nil. Every class about the database
// itself, other than a SQLSTATE, starts "db_".
//
// IT EXISTS FOR LOG LINES THAT ARE KEPT. The text of a failed write names the
// client's address and port, the text of a failed connect names the database's
// host, port and user, and an error wrapped further up could carry whatever its
// wrapper put in it, such as a buyer's email address. The Holder Export's
// "finished" audit line records an abort by this class (ADR 0075), and every
// request line that failed on an unmapped error carries it.
//
// JOINS ARE WALKED. errors.Is and errors.As already look inside an
// errors.Join, and pgconn joins one error per address it tried to reach, so a
// database that is down is classified by what went wrong at the socket - not
// named `*errors.joinError`, which is what a walk by errors.Unwrap alone gives,
// since Unwrap returns nil on a join.
func ErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	if class, ok := knownClass(err); ok {
		return class
	}
	return specificType(err)
}

// knownClass is the class of an error ErrorClass recognises, and false for
// one it does not.
//
// A failed connect to Postgres is checked first, because it carries the same
// socket and deadline errors a failed write or a request's end does - net's
// dial timeout even reads as context.DeadlineExceeded - and "the database could
// not be reached" is the fact a reader needs.
func knownClass(err error) (string, bool) {
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return databaseConnectClass(err), true
	}
	return failureClass(err)
}

// failureClass is the class of any recognised error but a failed connect to
// Postgres, and false for one it does not recognise. Order is priority: every
// check looks through the whole tree, wraps and joins alike, so the first that
// matches anywhere in it wins.
func failureClass(err error) (string, bool) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded", true
	case isCanceled(err):
		return canceledClass, true
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "write_deadline_exceeded", true
	case errors.Is(err, syscall.EPIPE):
		return "broken_pipe", true
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset", true
	}
	if class, ok := postgresClass(err); ok {
		return class, true
	}
	switch {
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, sql.ErrConnDone):
		return "db_connection_lost", true
	case errors.Is(err, errDatabaseClosed):
		return "db_closed", true
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "unexpected_eof", true
	case isRefused(err):
		return "connection_refused", true
	case isDNSError(err):
		return "dns_unresolved", true
	case isDialError(err):
		return "dial_failed", true
	}
	return "", false
}

// databaseConnectClass is the class of a failed connect to Postgres: why the
// database could not be reached, never where it is. It asks the same
// questions failureClass does, through the same predicates, so the two cannot
// disagree on what a SQLSTATE, a refusal, a name that does not resolve or a
// cancellation is.
//
// ITS ORDER IS ITS OWN. pgconn joins one error per address it tried, so one
// connect can fail several ways at once, and the class names the most telling:
// a server's own answer, then a refusal, then a name that does not resolve,
// and running out of time last, since that says least about why. failureClass
// ranks a deadline first, which is right for a write and wrong here.
func databaseConnectClass(err error) string {
	// The server answered and refused, for example while starting up (57P03)
	// or on a bad password (28P01). Its SQLSTATE says which.
	if class, ok := postgresClass(err); ok {
		return class
	}
	switch {
	case isRefused(err):
		return "db_connect_refused"
	case isDNSError(err):
		return "db_dns_unresolved"
	// The reader leaving while the connect was still dialling is the reader's
	// doing, and is named as every other cancellation is.
	case isCanceled(err):
		return canceledClass
	// A dial that ran out of time, whether its own connect timeout or the
	// request's deadline ended it.
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded), isTimeout(err):
		return "db_connect_timeout"
	}
	return "db_unavailable"
}

// canceledClass is a context's cancellation, the same class wherever it ends
// the work.
const canceledClass = "context_canceled"

// postgresClass is "postgres " and the SQLSTATE of a Postgres error in err's
// tree, and false when there is none.
func postgresClass(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "postgres " + pgErr.Code, true
	}
	return "", false
}

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func isRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

func isDialError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

// errDatabaseClosed is database/sql's own "sql: database is closed", which it
// does not export. It is captured once from a closed pool rather than matched
// by its text.
var errDatabaseClosed = func() error {
	db := sql.OpenDB(refusingConnector{})
	_ = db.Close()
	_, err := db.Conn(context.Background())
	return err
}()

// refusingConnector is a driver.Connector that never connects. It exists only
// so errDatabaseClosed can be read off a pool that never opens a connection.
type refusingConnector struct{}

func (refusingConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("refusingConnector never connects")
}

func (refusingConnector) Driver() driver.Driver { return nil }

// specificType is the Go type of the most specific error in err's tree.
func specificType(err error) string {
	return fmt.Sprintf("%T", specificError(err))
}

// specificError is the most specific error in err's tree. Down a chain of
// single wraps that is the innermost one. At a join, it is the first member
// whose own most specific error is not one of the standard library's generic
// wrappers, so a join of a wrapped sentinel and a typed error is named by the
// type; failing that, the first member's.
func specificError(err error) error {
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		var first error
		for _, member := range e.Unwrap() {
			if member == nil {
				continue
			}
			specific := specificError(member)
			if !genericTypes[reflect.TypeOf(specific)] {
				return specific
			}
			if first == nil {
				first = specific
			}
		}
		if first != nil {
			return first
		}
	case interface{ Unwrap() error }:
		if inner := e.Unwrap(); inner != nil {
			return specificError(inner)
		}
	}
	return err
}

// genericTypes are the standard library's anonymous error types, which say
// nothing about what failed. They are read off values the standard library
// builds rather than written out as type names, so a rename there cannot
// quietly leave this set matching nothing.
var genericTypes = map[reflect.Type]bool{
	reflect.TypeOf(errors.New("")):                                      true, // *errors.errorString
	reflect.TypeOf(errors.Join(errors.New(""))):                         true, // *errors.joinError
	reflect.TypeOf(fmt.Errorf("%w", errors.New(""))):                    true, // *fmt.wrapError
	reflect.TypeOf(fmt.Errorf("%w %w", errors.New(""), errors.New(""))): true, // *fmt.wrapErrors
}
