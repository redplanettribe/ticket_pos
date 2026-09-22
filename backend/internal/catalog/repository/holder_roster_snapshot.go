package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// The HOLDER EXPORT's snapshot (ADR 0075): every read the file makes, inside one
// read-only REPEATABLE READ transaction, with the roster read through a cursor.
//
// ONE MOMENT AND NOT A SEQUENCE OF PAGES. The export streams for as long as its
// client takes to download, and a sale, a reassignment or an acceptance made in
// that time must not reach the file: under the `holder` or `buyer` sort a change
// moves a Ticket across a page boundary, and a roster that silently repeats or
// loses a person is the outcome ADR 0065 exists to prevent. Inside the snapshot
// the file is exactly the roster as it stood when the first statement ran, and
// the Event's questions are read at that same moment.
//
// A CURSOR AND NOT ONE OPEN RESULT SET, because the Answers are read alongside
// the Tickets in bounded batches and a transaction's connection cannot run a
// second statement while a first one is still streaming rows. FETCH hands the
// roster over holderRosterBatch Tickets at a time, and between two fetches the
// same connection reads that batch's Answers - so memory is one batch, however
// many Tickets the Event holds.
//
// IT HOLDS ONE POOLED CONNECTION until Close, for as long as the download takes.
// That is the price ADR 0075 names, and the concurrency bound is the service's.

// querier is what the snapshot's reads and the pool-backed ones share: a pool or
// a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// holderRosterBatch is how many Tickets one FETCH reads, and so how many Tickets'
// Answers one query reads: small enough that a batch of the widest Event is a
// few MiB, large enough that a large roster is not a round trip per row.
const holderRosterBatch = 500

// HolderRosterSnapshot is an open Holder Export read. Close it, always.
type HolderRosterSnapshot struct {
	tx     *sql.Tx
	cursor bool
	done   bool
}

// OpenHolderRosterSnapshot begins the export's read-only REPEATABLE READ
// transaction and fixes its snapshot before returning.
//
// Postgres takes a REPEATABLE READ snapshot at the transaction's first
// statement, not at BEGIN, so this runs one. That makes the moment Open returns
// the snapshot's moment: a caller that reads the time right after it has the
// generation time the file is a picture of, whichever read it makes first.
func (r *Repository) OpenHolderRosterSnapshot(ctx context.Context) (*HolderRosterSnapshot, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return &HolderRosterSnapshot{tx: tx}, nil
}

// ListEventTicketQuestions is the repository's read of the same name, at the
// snapshot's moment.
func (s *HolderRosterSnapshot) ListEventTicketQuestions(ctx context.Context, orgID, eventID string) ([]EventTicketQuestion, error) {
	return listEventTicketQuestions(ctx, s.tx, orgID, eventID)
}

// ListTicketTypesByEventID is the repository's read of the same name, at the
// snapshot's moment.
func (s *HolderRosterSnapshot) ListTicketTypesByEventID(ctx context.Context, orgID, eventID string) ([]TicketType, error) {
	return listTicketTypesByEventID(ctx, s.tx, orgID, eventID)
}

// OpenRoster declares the cursor over the roster q names - holderRosterSelect,
// the Holder List's own statement, with no page on it. Limit and Offset are
// ignored: the file is the whole view.
func (s *HolderRosterSnapshot) OpenRoster(ctx context.Context, q ListHolderTicketsQuery) error {
	if s.cursor {
		return errors.New("catalog: the Holder Export roster is already open")
	}
	where, args := holderRosterFilters(q)
	roster := holderRosterSelect(q, where)
	if _, err := s.tx.ExecContext(ctx, `DECLARE holder_export NO SCROLL CURSOR FOR `+roster, args...); err != nil {
		return err
	}
	s.cursor = true
	return nil
}

// NextTickets returns the roster's next batch, in the roster's order, or an
// empty slice once it is exhausted.
func (s *HolderRosterSnapshot) NextTickets(ctx context.Context) ([]HolderTicket, error) {
	if !s.cursor {
		return nil, errors.New("catalog: the Holder Export roster is not open")
	}
	if s.done {
		return nil, nil
	}
	rows, err := s.tx.QueryContext(ctx, fmt.Sprintf(`FETCH FORWARD %d FROM holder_export`, holderRosterBatch))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tickets := make([]HolderTicket, 0, holderRosterBatch)
	for rows.Next() {
		t, err := scanHolderTicket(rows)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.done = len(tickets) < holderRosterBatch
	return tickets, nil
}

// ListTicketAnswers is the repository's read of the same name, at the
// snapshot's moment.
func (s *HolderRosterSnapshot) ListTicketAnswers(ctx context.Context, ticketIDs []string) ([]TicketAnswer, error) {
	return listTicketAnswers(ctx, s.tx, ticketIDs)
}

// Close ends the snapshot and gives its connection back. It is a rollback: the
// transaction is read-only and has nothing to commit.
func (s *HolderRosterSnapshot) Close() error {
	err := s.tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		// Already ended by its context being cancelled; the connection is back.
		return nil
	}
	return err
}
