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

	"github.com/jackc/pgx/v5/pgconn"
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
