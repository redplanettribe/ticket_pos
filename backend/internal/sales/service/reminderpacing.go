package service

import (
	"context"
	"time"
)

// Pacing for the Reminder sweeps (#376).
//
// Both sweeps — Answer Reminder and Assignment Reminder — write once per
// reader, and until this file did so in a tight loop. The mail provider
// allows ten requests a second, and the Assignment Reminder's launch run
// (ADR 0051, #371) found the limit the only way a tight loop can: eighteen
// due, fifteen sent, three refused with a 429. A refused send leaves no
// ledger row, which is correct — the reader was not written to — but on the
// scheduled cadence the retry is the NEXT DAY's tick, so any run longer than
// about ten mails loses its tail to tomorrow.
//
// THE FIX IS A GAP, NOT A RETRY. A fixed pause between consecutive requests
// keeps a run under the limit by construction, needs no reading of the
// provider's retry-after and leaves the accounting exactly as it was: a send
// that still fails is still counted failed, still unrecorded, still due. At
// reminderSendGap a full batch of fifty takes about six seconds, well inside
// the sixty-second budget both sweeps keep, and that budget is still what
// bounds a run: the pause is checked against it before it is spent, so a slow
// provider plus the gap stops the run cleanly with the remainder reported as
// backlog rather than overrunning the scheduler's deadline.
//
// THE GAP FOLLOWS A REQUEST, NOT AN OUTCOME. What the provider rations is
// requests, so a refused send is paced like an accepted one, and a candidate
// the sweep skipped before asking the provider anything — no address, no
// link — costs no pause at all. Nothing precedes the first request: a run of
// one mail is as quick as it ever was.

// reminderSendGap is the pause between consecutive provider requests. Eight
// a second against a limit of ten, leaving room for whatever else the
// deployment is sending at the same moment — a receipt, a passcode.
const reminderSendGap = 120 * time.Millisecond

// sleepUnlessCancelled is the deployed sleep: a real wait that a cancelled
// context cuts short, so a dying process does not hold its last sweep open.
func sleepUnlessCancelled(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// reminderPacer keeps the gap between one sweep's requests. It is told how
// many requests have reached the provider so far and pauses before the next
// one only when the count has moved since it last looked, which is how a skip
// costs nothing and the first request is never preceded by a wait.
//
// The pause never runs past the deadline: a gap that would is cut to what is
// left, and the sweep's own budget check, made right after, is what stops the
// run. The pacer does not decide that; it only refuses to sleep through it.
type reminderPacer struct {
	service  *Service
	deadline time.Time
	paced    int // requests the last pause accounted for
}

func (p *reminderPacer) pauseBefore(ctx context.Context, requestsSoFar int) {
	if requestsSoFar == p.paced {
		return
	}
	p.paced = requestsSoFar
	gap := p.service.sendGap
	if gap <= 0 {
		return
	}
	if remaining := p.deadline.Sub(p.service.now()); remaining < gap {
		gap = remaining
	}
	if gap <= 0 {
		return
	}
	p.service.sleep(ctx, gap)
}
