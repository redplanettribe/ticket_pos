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
		return noneClass
	}
	if class, ok := knownClass(err); ok {
		return class
	}
	return specificType(err)
}

// The classes ErrorClass gives, other than a Go type's name.
const (
	noneClass = "none"

	// A context's end, the same class wherever it ends the work.
	deadlineClass = "context_deadline_exceeded"
	canceledClass = "context_canceled"

	// A Postgres error, followed by its SQLSTATE.
	postgresClassPrefix = "postgres "

	// A socket, anywhere but a connect to the database.
	writeDeadlineClass     = "write_deadline_exceeded"
	brokenPipeClass        = "broken_pipe"
	connectionResetClass   = "connection_reset"
	unexpectedEOFClass     = "unexpected_eof"
	connectionRefusedClass = "connection_refused"
	dnsUnresolvedClass     = "dns_unresolved"
	dialFailedClass        = "dial_failed"

	// The database.
	dbConnectionLostClass = "db_connection_lost"
	dbClosedClass         = "db_closed"
	dbConnectRefusedClass = "db_connect_refused"
	dbDNSUnresolvedClass  = "db_dns_unresolved"
	dbConnectTimeoutClass = "db_connect_timeout"
	dbUnavailableClass    = "db_unavailable"
)

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
// matches anywhere in it wins. A context's end ranks above everything else,
// so a cancellation outranks a refusal here as it does in
// databaseConnectClass. A Postgres SQLSTATE ranks next, above every socket
// failure, as it ranks first in databaseConnectClass: the server's own answer
// says more than a timeout or reset found beside it. A lookup that a context ended is never a name that
// does not resolve, here or there, because isDNSError does not count it: net
// reports such a lookup as a *net.DNSError that unwraps to the context's error.
func failureClass(err error) (string, bool) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return deadlineClass, true
	case errors.Is(err, context.Canceled):
		return canceledClass, true
	case errors.As(err, &pgErr):
		return postgresClassPrefix + pgErr.Code, true
	case errors.Is(err, os.ErrDeadlineExceeded):
		return writeDeadlineClass, true
	case errors.Is(err, syscall.EPIPE):
		return brokenPipeClass, true
	case errors.Is(err, syscall.ECONNRESET):
		return connectionResetClass, true
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, sql.ErrConnDone):
		return dbConnectionLostClass, true
	case errors.Is(err, errDatabaseClosed):
		return dbClosedClass, true
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return unexpectedEOFClass, true
	case errors.Is(err, syscall.ECONNREFUSED):
		return connectionRefusedClass, true
	case isDNSError(err):
		return dnsUnresolvedClass, true
	case isDialError(err):
		return dialFailedClass, true
	}
	return "", false
}

// databaseConnectClass is the class of a failed connect to Postgres: why the
// database could not be reached, never where it is. Each of its checks is one
// failureClass also makes, written the same way (the same errors.Is target,
// the same errors.As type, the same isDNSError), so the two agree on what a
// SQLSTATE, a refusal, a name that does not resolve or a cancellation is; keep
// them written alike. A check that is a single errors.Is or errors.As is
// written inline; one that needs more has a predicate below. Its timeout check
// is the one exception: it folds a context's deadline, a socket's deadline and
// any Timeout() error into one class, where failureClass tells a context's
// deadline from a write's.
//
// ITS ORDER IS ITS OWN. pgconn joins one error per address it tried, so one
// connect can fail several ways at once, and the class names the most
// telling. failureClass ranks a deadline first, which is right for a write and
// wrong here.
func databaseConnectClass(err error) string {
	var pgErr *pgconn.PgError
	switch {
	// The server answered and refused, for example while starting up (57P03)
	// or on a bad password (28P01). Its SQLSTATE says which.
	case errors.As(err, &pgErr):
		return postgresClassPrefix + pgErr.Code
	// The request itself was cancelled: the reader left while the connect was
	// still resolving or dialling, and that is the truth about why it failed,
	// whatever else the other addresses said.
	case errors.Is(err, context.Canceled):
		return canceledClass
	// A refusal outranks a name that does not resolve: the server's address
	// was known and something there answered no.
	case errors.Is(err, syscall.ECONNREFUSED):
		return dbConnectRefusedClass
	// Only a lookup that finished and failed. One a deadline cut off is a
	// timeout below, because the name was never found to be missing.
	case isDNSError(err):
		return dbDNSUnresolvedClass
	// A dial or a lookup that ran out of time, whether its own connect timeout
	// or the request's deadline ended it. Last, since it says least about why.
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded), isTimeout(err):
		return dbConnectTimeoutClass
	}
	return dbUnavailableClass
}

// isDNSError reports whether err's tree, wraps and joins alike, holds a
// *net.DNSError for a lookup that finished and failed. A lookup a context
// ended is not one: net builds it as newDNSError(mapErr(ctx.Err()), ...), a
// *net.DNSError that unwraps to context.Canceled or context.DeadlineExceeded,
// and the name it looked for was never found to be missing. errors.As cannot
// ask this, since it stops at the first *net.DNSError, and a join may hold a
// lookup that timed out before one that found no such host.
func isDNSError(err error) bool {
	if dnsErr, ok := err.(*net.DNSError); ok && dnsErr != nil && !isContextEnd(dnsErr) {
		return true
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		for _, member := range e.Unwrap() {
			if isDNSError(member) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return isDNSError(e.Unwrap())
	}
	return false
}

func isContextEnd(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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
