package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// kickLogEntry is one line the kick's logger was handed.
type kickLogEntry struct {
	level string
	msg   string
	args  []any
}

func (e kickLogEntry) attr(key string) any {
	for i := 0; i+1 < len(e.args); i += 2 {
		if e.args[i] == key {
			return e.args[i+1]
		}
	}
	return nil
}

// kickLogger hands over the first line at Warn or Error, which is how a kick
// that ran in the background reports its end.
type kickLogger struct {
	done chan kickLogEntry
}

func newKickLogger() *kickLogger { return &kickLogger{done: make(chan kickLogEntry, 1)} }

func (l *kickLogger) log(level, msg string, args []any) {
	if level == "INFO" {
		return
	}
	select {
	case l.done <- kickLogEntry{level: level, msg: msg, args: args}:
	default:
	}
}

func (l *kickLogger) Info(msg string, args ...any)  { l.log("INFO", msg, args) }
func (l *kickLogger) Warn(msg string, args ...any)  { l.log("WARN", msg, args) }
func (l *kickLogger) Error(msg string, args ...any) { l.log("ERROR", msg, args) }

func (l *kickLogger) await(t *testing.T) kickLogEntry {
	t.Helper()
	select {
	case entry := <-l.done:
		return entry
	case <-time.After(5 * time.Second):
		t.Fatal("the kick never reported how it ended")
		return kickLogEntry{}
	}
}

// closedRepository is a Repository whose every query fails, without a
// database: the pool is closed before it is used.
func closedRepository(t *testing.T) *repository.Repository {
	t.Helper()
	pool, err := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = pool.Close()
	return repository.New(&platform.DB{Pool: pool})
}

// A panic inside the fire-and-forget kick would take the whole API process
// down with it. It is recovered and logged at ERROR with its stack, and the
// process, here the test binary, carries on (#763, ADR 0079).
func TestTheSaleInvoiceDrainerKickRecoversAPanicAndLogsItsStack(t *testing.T) {
	logger := newKickLogger()
	svc := New(closedRepository(t), nil, logger).WithClock(func() time.Time {
		panic("the clock broke")
	})

	svc.KickSaleInvoiceDrainer(context.Background(), "sale-1")

	entry := logger.await(t)
	if entry.level != "ERROR" {
		t.Fatalf("a panicking kick logged %s %q, want ERROR", entry.level, entry.msg)
	}
	if got := fmt.Sprint(entry.attr("panic")); got != "the clock broke" {
		t.Fatalf("logged panic = %q, want the panic's value", got)
	}
	if stack, _ := entry.attr("stack").(string); !strings.Contains(stack, "drainSaleInvoices") {
		t.Fatalf("logged stack = %q, want one through the drain that panicked", stack)
	}
}

// A kick that fails is an alerting matter, since a silent kick is how a Sale
// Invoice sits pending: it logs at ERROR, not WARN (#763, ADR 0079).
func TestAFailedSaleInvoiceDrainerKickLogsAtError(t *testing.T) {
	logger := newKickLogger()
	svc := New(closedRepository(t), nil, logger)

	svc.KickSaleInvoiceDrainer(context.Background(), "sale-1")

	entry := logger.await(t)
	if entry.level != "ERROR" {
		t.Fatalf("a failed kick logged %s %q, want ERROR", entry.level, entry.msg)
	}
	if err, _ := entry.attr("error").(error); err == nil {
		t.Fatalf("a failed kick logged %v, want the error", entry.args)
	}
}
