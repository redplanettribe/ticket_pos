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
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// An error is logged as a CLASS and never as its text where the text could
// carry somebody's data: the text of a failed write names the client's address
// and port, and an error wrapped further up could carry whatever its wrapper put
// in it. Every class ErrorClass can give is named here.
func TestErrorClassNamesTheKindAndCarriesNoErrorText(t *testing.T) {
	peer := &net.TCPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 51234}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no error", nil, "none"},
		{"a deadline", fmt.Errorf("fetch: %w", context.DeadlineExceeded), "context_deadline_exceeded"},
		{"a cancellation", fmt.Errorf("fetch: %w", context.Canceled), "context_canceled"},
		{"a write that timed out",
			&net.OpError{Op: "write", Net: "tcp", Addr: peer, Err: os.ErrDeadlineExceeded}, "write_deadline_exceeded"},
		{"a closed pipe",
			&net.OpError{Op: "write", Net: "tcp", Addr: peer, Err: syscall.EPIPE}, "broken_pipe"},
		{"a client that went away",
			&net.OpError{Op: "write", Net: "tcp", Addr: peer, Err: syscall.ECONNRESET}, "connection_reset"},
		{"a Postgres refusal",
			fmt.Errorf("fetch ana@example.com: %w", &pgconn.PgError{Code: "57P01", Message: "terminating ana@example.com"}),
			"postgres 57P01"},
		{"a bad driver connection", fmt.Errorf("fetch ana@example.com: %w", driver.ErrBadConn), "database_connection_lost"},
		{"a connection already closed", fmt.Errorf("fetch: %w", sql.ErrConnDone), "database_connection_lost"},
		{"a cut read", fmt.Errorf("read 203.0.113.7:51234: %w", io.ErrUnexpectedEOF), "unexpected_eof"},
		{"an end of file", fmt.Errorf("read: %w", io.EOF), "unexpected_eof"},
		{"anything else, by its innermost type",
			fmt.Errorf("row for ana@example.com: %w", errors.New("boom 203.0.113.7:51234")), "*errors.errorString"},
		{"a refused connection outside the database",
			&net.OpError{Op: "dial", Net: "tcp", Addr: peer, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)},
			"connection_refused"},
		{"a refused connection inside a join",
			fmt.Errorf("fetch ana@example.com: %w", errors.Join(
				errors.New("first try 203.0.113.7:51234"),
				&net.OpError{Op: "dial", Net: "tcp", Addr: peer, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)},
			)),
			"connection_refused"},
		{"a name that does not resolve",
			&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "ana.example.com"}},
			"dns_unresolved"},
		{"a dial that failed otherwise",
			&net.OpError{Op: "dial", Net: "tcp", Addr: peer, Err: errors.New("network unreachable 203.0.113.7")},
			"dial_failed"},
		{"a known class in any member of a join",
			errors.Join(errors.New("boom ana@example.com"), fmt.Errorf("read: %w", io.ErrUnexpectedEOF)),
			"unexpected_eof"},
		{"an unknown join, by its first member when none is more specific",
			fmt.Errorf("fetch ana@example.com: %w", errors.Join(
				errors.New("boom 203.0.113.7:51234"),
				fmt.Errorf("wrapped: %w", errors.New("boom ana@example.com")),
			)),
			"*errors.errorString"},
		{"an unknown join with a typed member, by that type",
			errors.Join(errors.New("boom 203.0.113.7:51234"), fmt.Errorf("wrapped: %w", customErr{})),
			"platform.customErr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ErrorClass(tc.err)
			if got != tc.want {
				t.Errorf("class = %q, want %q", got, tc.want)
			}
			for _, leak := range []string{"203.0.113.7", "51234", "ana@example.com", "boom", "terminating"} {
				if strings.Contains(got, leak) {
					t.Errorf("class %q carries %q", got, leak)
				}
			}
		})
	}
}

type customErr struct{}

func (customErr) Error() string { return "boom ana@example.com 203.0.113.7:51234" }

// A DATABASE THAT IS DOWN IS NAMED, NOT CALLED A JOIN. pgconn joins one error
// per address it tried, and a walk by errors.Unwrap stops at the join, so every
// request logged error_class="*errors.joinError" while Postgres was down. These
// connect for real, through the pgx driver the API itself uses, to a port
// nothing listens on, so the error has the exact shape production sees - and
// the database's host, port and user are in its text, and never in its class.
func TestErrorClassNamesADatabaseThatCannotBeReached(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	url := "postgres://ana%40example.com@127.0.0.1:" + port + "/tickets?sslmode=disable&connect_timeout=5"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	assertClass := func(t *testing.T, err error, want string) {
		t.Helper()
		if err == nil {
			t.Fatal("connect succeeded, want a failure")
		}
		got := ErrorClass(err)
		if got != want {
			t.Fatalf("class = %q, want %q (error: %v)", got, want, err)
		}
		for _, leak := range []string{"127.0.0.1", port, "ana@example.com", "ana%40example.com", "tickets"} {
			if strings.Contains(got, leak) {
				t.Errorf("class %q carries %q", got, leak)
			}
		}
	}

	t.Run("through database/sql, wrapped by a repository", func(t *testing.T) {
		db, err := sql.Open("pgx", url)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer func() { _ = db.Close() }()
		_, err = db.ExecContext(ctx, "SELECT 1")
		var connectErr *pgconn.ConnectError
		if !errors.As(err, &connectErr) {
			t.Fatalf("error is not a pgconn.ConnectError: %T %v", err, err)
		}
		assertClass(t, fmt.Errorf("fetch sales for ana@example.com: %w", err), "db_connect_refused")
	})

	t.Run("straight from pgconn", func(t *testing.T) {
		_, err := pgconn.Connect(ctx, url)
		assertClass(t, err, "db_connect_refused")
	})

	t.Run("a pool already closed", func(t *testing.T) {
		db, err := sql.Open("pgx", url)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		_ = db.Close()
		_, err = db.ExecContext(ctx, "SELECT 1")
		assertClass(t, fmt.Errorf("fetch: %w", err), "db_closed")
	})

	t.Run("a connect the reader gave up on", func(t *testing.T) {
		gone, cancel := context.WithCancel(ctx)
		cancel()
		_, err := pgconn.Connect(gone, url)
		assertClass(t, err, "context_canceled")
	})
}

// A connect that fails for a reason the class has no word for is still named
// as a database that cannot be reached, never by the type of what it joined.
func TestErrorClassNamesAnyOtherFailedConnectAsUnavailable(t *testing.T) {
	// A server that accepts and hangs up before the startup reply.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = pgconn.Connect(ctx, "postgres://u@"+ln.Addr().String()+"/db?sslmode=disable&connect_timeout=5")
	if err == nil {
		t.Fatal("connect succeeded, want a failure")
	}
	if got := ErrorClass(err); got != "db_unavailable" {
		t.Fatalf("class = %q, want db_unavailable (error: %v)", got, err)
	}
}
