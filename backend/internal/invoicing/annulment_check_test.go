package invoicing

import (
	"testing"
	"time"
)

var recordedAt = time.Date(2026, time.August, 10, 14, 2, 0, 0, time.UTC)

func ask(after time.Duration, reading StatusReading) AnnulmentAsk {
	return AnnulmentAsk{At: recordedAt.Add(after), Reading: reading}
}

func manualAsk(after time.Duration, reading StatusReading) AnnulmentAsk {
	a := ask(after, reading)
	a.Manual = true
	return a
}

// TestAnnulmentVerdictRule tables the verdict rule, one row per answer
// sequence (#746, spec #742): confirmed on any ANULADO whatever came before;
// mismatch on AUTORIZADO from two different asks, the later at or after +30
// min; unconfirmed once an ask at or after +60 min is done with neither;
// otherwise checking. Since is the ask that reached the verdict.
func TestAnnulmentVerdictRule(t *testing.T) {
	const (
		A = StatusAnnulled
		Z = StatusAuthorized
		N = StatusNone
	)
	m := time.Minute
	cases := []struct {
		name    string
		asks    []AnnulmentAsk
		verdict AnnulmentVerdict
		since   time.Duration
	}{
		{"no asks yet", nil, AnnulmentChecking, 0},
		{"one no reading", []AnnulmentAsk{ask(m, N)}, AnnulmentChecking, 0},
		{"ANULADO on the first ask", []AnnulmentAsk{ask(m, A)}, AnnulmentConfirmed, m},
		{"lag: AUTORIZADO then ANULADO", []AnnulmentAsk{ask(m, Z), ask(5*m, A)}, AnnulmentConfirmed, 5 * m},
		{"ANULADO after a mismatch", []AnnulmentAsk{ask(m, Z), ask(30*m, Z), ask(60*m, A)}, AnnulmentConfirmed, 60 * m},
		{"ANULADO stays confirmed after a later AUTORIZADO", []AnnulmentAsk{ask(m, A), manualAsk(2*m, Z), ask(30*m, Z)}, AnnulmentConfirmed, m},
		{"one AUTORIZADO is never a mismatch", []AnnulmentAsk{ask(m, N), ask(5*m, N), ask(15*m, N), ask(30*m, Z)}, AnnulmentChecking, 0},
		{"two AUTORIZADO both before +30 min", []AnnulmentAsk{ask(m, Z), ask(5*m, Z), ask(15*m, Z)}, AnnulmentChecking, 0},
		{"two manual AUTORIZADO pressed in quick succession", []AnnulmentAsk{manualAsk(2*m, Z), manualAsk(2*m+10*time.Second, Z)}, AnnulmentChecking, 0},
		{"two AUTORIZADO across +30 min", []AnnulmentAsk{ask(m, Z), ask(5*m, N), ask(30*m, Z)}, AnnulmentMismatch, 30 * m},
		{"the later AUTORIZADO exactly at +30 min", []AnnulmentAsk{ask(15*m, Z), ask(30*m, Z)}, AnnulmentMismatch, 30 * m},
		{"the later AUTORIZADO a second short of +30 min", []AnnulmentAsk{ask(15*m, Z), ask(30*m-time.Second, Z)}, AnnulmentChecking, 0},
		{"a manual AUTORIZADO counts as one of the two", []AnnulmentAsk{manualAsk(3*m, Z), ask(30*m, Z)}, AnnulmentMismatch, 30 * m},
		{"both AUTORIZADO after +30 min", []AnnulmentAsk{ask(30*m, Z), ask(60*m, Z)}, AnnulmentMismatch, 60 * m},
		{"all no readings through +60 min", []AnnulmentAsk{ask(m, N), ask(5*m, N), ask(15*m, N), ask(30*m, N), ask(60*m, N)}, AnnulmentUnconfirmed, 60 * m},
		{"one AUTORIZADO through +60 min is unconfirmed", []AnnulmentAsk{ask(m, N), ask(5*m, Z), ask(15*m, N), ask(30*m, N), ask(60*m, N)}, AnnulmentUnconfirmed, 60 * m},
		{"no readings until +59 min are still checking", []AnnulmentAsk{ask(m, N), ask(30*m, N), ask(59*m, N)}, AnnulmentChecking, 0},
		{"unconfirmed turns into mismatch", []AnnulmentAsk{ask(m, N), ask(60*m, N), ask(7*time.Hour, Z), ask(13*time.Hour, Z)}, AnnulmentMismatch, 13 * time.Hour},
		{"unconfirmed with one late AUTORIZADO stays unconfirmed", []AnnulmentAsk{ask(60*m, N), ask(7*time.Hour, Z)}, AnnulmentUnconfirmed, 60 * m},
		{"mismatch never returns to unconfirmed", []AnnulmentAsk{ask(m, Z), ask(30*m, Z), ask(60*m, N), ask(7*time.Hour, N), ask(13*time.Hour, N)}, AnnulmentMismatch, 30 * m},
		{"mismatch reached at +60 min rather than unconfirmed", []AnnulmentAsk{ask(m, Z), ask(30*m, N), ask(60*m, Z)}, AnnulmentMismatch, 60 * m},
		{"unconfirmed then ANULADO", []AnnulmentAsk{ask(60*m, N), ask(7*time.Hour, A)}, AnnulmentConfirmed, 7 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AnnulmentVerdictOf(recordedAt, c.asks)
			if got.Verdict != c.verdict {
				t.Fatalf("verdict = %s; want %s", got.Verdict, c.verdict)
			}
			if c.verdict == AnnulmentChecking {
				if !got.Since.IsZero() {
					t.Fatalf("since = %s; want none while checking", got.Since)
				}
				return
			}
			if want := recordedAt.Add(c.since); !got.Since.Equal(want) {
				t.Fatalf("since = +%s; want +%s", got.Since.Sub(recordedAt), c.since)
			}
		})
	}
}

// TestAnnulmentVerdictReadsTheAsksInOrderWhateverTheyArriveIn: the rule is
// over the ordered asks, so a slice handed to it out of order is read by time.
func TestAnnulmentVerdictReadsTheAsksInOrderWhateverTheyArriveIn(t *testing.T) {
	got := AnnulmentVerdictOf(recordedAt, []AnnulmentAsk{ask(30*time.Minute, StatusAuthorized), ask(time.Minute, StatusAuthorized)})
	if got.Verdict != AnnulmentMismatch || !got.Since.Equal(recordedAt.Add(30*time.Minute)) {
		t.Fatalf("got %+v; want mismatch since +30 min", got)
	}
}

// TestAnnulmentCheckSchedule: asks fall due at +1, +5, +15, +30 and +60 min,
// then every 6 h until the later of recording + 7 days and the window's
// close, then none; a confirmed check asks nothing more. The next ask is the
// first rung strictly after the ask just made, so a late tick that covers two
// rungs asks once.
func TestAnnulmentCheckSchedule(t *testing.T) {
	m := time.Minute
	h := time.Hour
	early := recordedAt.Add(2 * 24 * h) // a window closing before +7 days
	late := recordedAt.Add(20*24*h + m) // a window closing after
	cases := []struct {
		name    string
		closes  time.Time
		verdict AnnulmentVerdict
		after   time.Duration
		next    time.Duration
		none    bool
	}{
		{"after the +1 min ask", early, AnnulmentChecking, m, 5 * m, false},
		{"after the +5 min ask", early, AnnulmentChecking, 5 * m, 15 * m, false},
		{"after the +15 min ask", early, AnnulmentChecking, 15 * m, 30 * m, false},
		{"after the +30 min ask", early, AnnulmentChecking, 30 * m, 60 * m, false},
		{"after the +60 min ask", early, AnnulmentUnconfirmed, 60 * m, 60*m + 6*h, false},
		{"a late tick covering two rungs asks once", early, AnnulmentChecking, 6 * m, 15 * m, false},
		{"a tick at +40 min next asks at +60", early, AnnulmentChecking, 40 * m, 60 * m, false},
		{"six-hourly at mismatch", early, AnnulmentMismatch, 60*m + 6*h, 60*m + 12*h, false},
		{"the last rung before +7 days", early, AnnulmentUnconfirmed, 60*m + 26*6*h, 60*m + 27*6*h, false},
		{"none after +7 days when the window closed earlier", early, AnnulmentUnconfirmed, 60*m + 27*6*h, 0, true},
		{"on past +7 days while the window is open", late, AnnulmentMismatch, 60*m + 27*6*h, 60*m + 28*6*h, false},
		{"the last rung before the window's close", late, AnnulmentMismatch, 60*m + 78*6*h, 60*m + 79*6*h, false},
		{"none after the window's close", late, AnnulmentMismatch, 60*m + 79*6*h, 0, true},
		{"confirmed asks nothing more", early, AnnulmentConfirmed, 5 * m, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next, ok := NextAnnulmentAsk(recordedAt, c.closes, c.verdict, recordedAt.Add(c.after))
			if c.none {
				if ok {
					t.Fatalf("next = +%s; want none", next.Sub(recordedAt))
				}
				return
			}
			if !ok || !next.Equal(recordedAt.Add(c.next)) {
				t.Fatalf("next = +%s (%v); want +%s", next.Sub(recordedAt), ok, c.next)
			}
		})
	}
	if first := FirstAnnulmentAsk(recordedAt); !first.Equal(recordedAt.Add(m)) {
		t.Fatalf("first ask = +%s; want +1 min", first.Sub(recordedAt))
	}
	if until := AnnulmentChecksUntil(recordedAt, early); !until.Equal(recordedAt.Add(7 * 24 * h)) {
		t.Fatalf("until = %s; want recording + 7 days", until)
	}
	if until := AnnulmentChecksUntil(recordedAt, late); !until.Equal(late) {
		t.Fatalf("until = %s; want the window's close", until)
	}
}
