package service

import (
	"context"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The staff gate on a LIVE session (#570, ADR 0067's amendment to ADR 0066).
//
// ADR 0066 had the gate bind at the next sign-in. In practice it never binds:
// the Staff Session's expiry slides on every authenticated request, so a daily
// user signs in once and never again, and "at the next sign-in" is unbounded
// rather than the fortnight it was taken to be. The gate therefore binds where
// the person actually is — on their next PAGE NAVIGATION — and does so within
// a minute of the edition's day arriving.
//
// What that costs, and what it deliberately does not:
//
//   - THE SESSION IS LEFT UNTOUCHED. Not revoked, not re-minted, and no
//     passcode is burned. Passcodes are rationed (10 per IP per 15 minutes,
//     sharing a platform ceiling with Sale Confirmations), so making somebody
//     re-prove an address they have already proven is making them spend
//     something they may not be able to get — during an event, for a document
//     they have not read yet.
//   - THE GATE NEVER BINDS ON `/api/`. It is a navigation gate and nothing
//     else: a sale in progress commits, and no in-flight mutation is refused
//     because a legal edition rolled over between the click and the write. The
//     staff app's middleware short-circuits `/api/` before it ever asks, and
//     the predicate below is not on any authenticated API path — which is also
//     why it is NOT computed in buildSessionView. A consent read that failed
//     there would 500 every staff API request, which is precisely the in-flight
//     refusal this rules out.
//   - PUBLISHING REVOKES NOTHING, in either population, and no control offers
//     to. Both session kinds are server-side rows and a DELETE would be easy;
//     it buys nothing from customers (already re-gated on a live session) and
//     from the staff it would force 25 passcodes through a limit of 10.
//
// The publishing operator is not exempt: `/operator` is checked against this
// gate too, so the person who re-gated everybody has demonstrably read the
// text they published.

// TermsOutstanding reports whether this email owes a Terms Acceptance — the
// gate predicate, and the one place it is spelled.
//
// MEMBERSHIP, NOT EQUALITY (#560): outstanding is "has accepted, as an
// organizer, NO edition that still satisfies the gate". A correction published
// under somebody's feet leaves them clear; a gating edition lifts the floor
// above every stored acceptance and re-gates everyone, with no code and no data
// migration.
//
// It reads the satisfying set ONLY, and never the current edition. Deciding
// who owes something is a question about the version rows; what to SHOW them is
// a second question, asked afterwards and only of the people who owe.
func (s *Service) TermsOutstanding(ctx context.Context, email string) (bool, error) {
	satisfying, err := s.termsVersions.SatisfyingTermsEditions(ctx)
	if err != nil {
		return false, err
	}
	accepted, err := s.repo.HasSatisfyingTermsAcceptance(ctx, email, satisfying.IDs())
	if err != nil {
		return false, err
	}
	return !accepted, nil
}

// StaffTermsGate is what a live Staff Session owes before its holder may see
// another page: the interstitial's box, or nil when they owe nothing.
//
// It is gateOnTerms verbatim — the same predicate, the same pinned edition, the
// same pinned label locale, the same fifteen-minute single-use token — because
// the two surfaces must ask the identical question and record the identical
// evidence. The only difference is what the token buys: at the sign-in door it
// buys the session that was withheld, and here it buys nothing, because the
// person already holds one.
//
// pageLocale is the language the staff app is rendered in for this reader. The
// label is served in it, floored at the prevailing text when this edition does
// not publish it (acceptanceLabel), and the locale ACTUALLY served comes back
// on the view so the link beside the box opens the document the words came
// from.
func (s *Service) StaffTermsGate(ctx context.Context, email string, pageLocale platform.Locale) (*TermsRequiredView, error) {
	return s.gateOnTerms(ctx, email, pageLocale, s.now())
}

// SessionTermsAcceptance is an interstitial being answered by somebody who is
// already signed in: the session doing the answering, the token that pinned
// what they were shown, the one box, and the circumstances to record.
type SessionTermsAcceptance struct {
	// SessionID is the live Staff Session, recorded as evidence and used for
	// nothing else. It is neither spent nor re-minted by this act.
	SessionID string
	// Email is the session's email, from the authenticated request and never
	// from the body.
	Email string
	// Token is the gate token from the interstitial that showed the box.
	Token string
	// TermsAcceptance is the required box.
	TermsAcceptance bool
	// Evidence is derived from the request by the handler, never from the body.
	Evidence consent.Evidence
}

// AcceptTermsOnSession records one Staff Terms Acceptance for somebody the
// navigation gate stopped, and returns them to what they were doing (#570).
//
// WHAT THIS DOES NOT DO IS THE POINT: it mints no session, spends no passcode,
// extends no authority and revokes nothing. The person was signed in before
// and is signed in after, with the same session row; all that changed is that
// the platform now holds their acceptance of the current edition.
//
// The edition and the language recorded are the ones PINNED TO THE TOKEN by
// the interstitial that rendered the box (#537's held-answer rule, #567's
// locale beside it). The request that ticks a box renders no text and knows
// neither.
//
// THE UNTICKED BOX IS JUDGED BEFORE THE TOKEN IS SPENT, which is the one place
// this departs from the sign-in door. There, spending first is deliberate: the
// token is a Proof of Email Ownership, and a refused submission must not be
// retriable against the same proof. Here the token proves nothing — the session
// is the credential — so burning it would only cost the person a page reload
// for having clicked the wrong thing, and the recovery from the sign-in door's
// version of that is a passcode this ticket exists to avoid spending.
func (s *Service) AcceptTermsOnSession(ctx context.Context, submission SessionTermsAcceptance) error {
	if !submission.TermsAcceptance {
		return identity.ErrTermsAcceptanceRequired()
	}

	pending, err := s.repo.ConsumePendingStaffTerms(ctx, submission.Token)
	if err != nil {
		return err
	}
	now := s.now()
	// Unknown, spent, expired, or issued to somebody else: one refusal,
	// deliberately indistinguishable. The email check is what stops a token
	// obtained for one address from being redeemed by another's session — it
	// would buy an acceptance in the wrong person's name, which is the only
	// thing a stolen gate token could ever be worth.
	if pending == nil || now.After(pending.ExpiresAt) ||
		!strings.EqualFold(pending.Email, submission.Email) {
		return identity.ErrPendingTermsInvalid()
	}

	return s.repo.InsertTermsAcceptance(ctx, repository.StaffTermsAcceptance{
		Email:           pending.Email,
		TermsVersionID:  pending.TermsVersionID,
		Capacity:        capacityOrganizer,
		AcceptedAt:      now,
		IP:              submission.Evidence.IP,
		UserAgent:       submission.Evidence.UserAgent,
		SessionID:       submission.SessionID,
		OriginURL:       submission.Evidence.OriginURL,
		PresentedLocale: pending.LabelLocale,
	})
}

// GetSessionWithTermsGate is GetSession plus the one fact the staff app's
// middleware needs to decide whether to divert this navigation to the
// interstitial (#570).
//
// It is a separate method, and the flag is a POINTER, because only this route
// asks. Every other caller of buildSessionView — SessionAuth on every staff API
// request, a verify, an organization switch — leaves it null, meaning "not
// asked" rather than "nothing owed". A plain false would be a field that lies
// on every response but one, and the middleware treats anything but an explicit
// true as no diversion.
//
// A FAILED GATE READ DOES NOT FAIL THE REQUEST, which is the opposite of what
// the sign-in door does with the same read, and deliberately. There, refusing
// costs a person a retry. Here, the session route's only failure mode is a 401
// to the middleware, which redirects to /login AND DELETES THE COOKIE — so a
// database hiccup would sign 25 people out and make them each spend a passcode,
// which is the exact harm this ticket forbids. So the flag is left null, the
// failure is logged, and this navigation proceeds; the next one asks again, and
// the acceptance is still owed at the sign-in door and at the interstitial,
// both of which fail closed.
func (s *Service) GetSessionWithTermsGate(ctx context.Context, sessionID string) (*SessionView, error) {
	view, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	outstanding, err := s.TermsOutstanding(ctx, view.Email)
	if err != nil {
		s.logger.Error("staff terms gate read", "error", err)
		return view, nil
	}
	view.TermsOutstanding = &outstanding
	return view, nil
}
