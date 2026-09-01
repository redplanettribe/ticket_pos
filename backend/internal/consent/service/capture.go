package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
)

// Capture records one act of asking a person what they authorize: the immutable
// Consent Record, the current state it makes true, and the Follow Digest flag
// kept in lockstep with Marketing Consent — all in one transaction.
//
// THIS IS THE PLATFORM'S ONLY CONSENT-WRITE PATH, and every capture surface in
// the parent feature is a caller of it: the sign-in consent step (#251), the
// online checkout (#253), the confirmation link that resolves a Pending
// Confirmation (#255), and the Customer Area toggle and unsubscribe link
// (#256). Nothing else writes `consent_records`, and nothing else writes the
// consent columns on `customers`. That is what makes the guarantees below
// properties of the platform rather than of a code path:
//
//   - A state change never happens without evidence of the act that caused it,
//     and evidence is never written without the state change committing with it.
//   - `digest_enabled` cannot drift from Marketing Consent, because the same
//     statement writes both (ADR 0034).
//   - A tick from an address nobody proved cannot become a lawful basis for
//     sending anything, because the granted/pending decision is made here from
//     an explicit input and not from the surface's own belief (ADR 0035).
//
// WHAT IT WRITES, from the three answers:
//
//	Policy Acceptance ticked  -> policy_accepted_at and policy_version_id are
//	                             stamped with the CURRENT edition, whether or not
//	                             the email was proven. It gates a purchase and
//	                             evidences that the person transacting was
//	                             informed; it is a fact about the act, not a
//	                             claim on somebody's inbox (ADR 0035).
//	Optional box, proven      -> ticked becomes granted, unticked becomes DENIED.
//	                             Silence at a capture moment is a No and is
//	                             recorded as one (ADR 0034). The write is
//	                             unconditional: a proven owner supersedes
//	                             anything a guest left behind.
//	Optional box, not proven  -> ticked becomes Pending Confirmation, written
//	                             only where the owner has not answered — over
//	                             NULL or over another Pending Confirmation, never
//	                             over a state written under a proven session.
//	                             UNTICKED WRITES NOTHING. Silence from somebody
//	                             who has not proven the address is not a No; it is
//	                             not an answer, and recording it as one would both
//	                             silence the Digest and stop the real owner from
//	                             ever being asked (see optionalStateWrite).
//	Box not shown (nil)       -> nothing is written for it, and the Consent
//	                             Record stores NULL. A Customer re-prompted after
//	                             a Policy Version bump sees the required box
//	                             alone, and their standing optional answers are
//	                             not churned.
//
// The Follow Digest flag moves only on a PROVEN answer: granted turns it on,
// denied turns it off, and everything else leaves it exactly as it was. Pending
// Confirmation must not enable it — that is the whole of ADR 0035 — and an
// unproven No must not disable it either, or a stranger typing a legacy
// subscriber's address into a checkout could silence their Digest. That second
// half is enforced above, by an unproven No writing no state at all: leaving the
// flag alone would not have been enough on its own, because the sender's rule
// (send when granted, or when unanswered and the flag is on; never when denied
// or pending) reads the consent state first and would have obeyed the denial.
//
// The Policy Version is resolved HERE, server-side, and is never a parameter.
// Which edition somebody accepted is the platform's finding about the moment,
// not the client's assertion about it — which is why the public policy endpoint
// publishes the label and the fingerprint but not the row's id.
//
// It opens its own transaction. CaptureInTx below is the same act inside one the
// caller already holds, for the checkout, where the record, the Customer it
// names and the sale it evidences must commit together.
func (s *Service) Capture(ctx context.Context, capture consent.Capture) (consent.Receipt, error) {
	return s.capture(ctx, nil, capture)
}

// CaptureInTx is Capture inside a transaction the caller already holds, for the
// surface whose capture act is one clause of a larger sentence.
//
// The online checkout is that surface (#253): its Consent Record, the Customer
// upsert it references and the Ticket Sale it evidences are three writes of one
// act, and they commit together or not at all. Everything else about the capture
// — the rules above, the resolved Policy Version, the granted/pending decision —
// is identical, because it is literally the same code: only who owns the
// transaction differs, and a second implementation of these rules is exactly
// what this method exists to avoid.
//
// The caller must not have written anything the capture depends on outside this
// transaction: the Customer named by CustomerID is read and updated here, so a
// checkout hands over the id its own upsert produced moments earlier in the very
// same tx.
func (s *Service) CaptureInTx(ctx context.Context, tx *sql.Tx, capture consent.Capture) (consent.Receipt, error) {
	if tx == nil {
		return consent.Receipt{}, errors.New("consent capture: no transaction")
	}
	return s.capture(ctx, tx, capture)
}

// capture is the whole of both: tx nil means "open one of your own".
func (s *Service) capture(ctx context.Context, tx *sql.Tx, capture consent.Capture) (consent.Receipt, error) {
	if capture.CustomerID == "" {
		return consent.Receipt{}, errors.New("consent capture: no customer")
	}
	// An unknown channel is a bug in a caller, refused here so the failure names
	// the mistake rather than surfacing as a CHECK constraint violation on a row
	// nobody can see.
	if !capture.Channel.Valid() {
		return consent.Receipt{}, fmt.Errorf("consent capture: unknown channel %q", capture.Channel)
	}
	// A withdraw-only channel may not grant, and the refusal is the write path's
	// rather than any surface's (#271). See refuseGrantOnOperatorRequest.
	if err := refuseGrantOnOperatorRequest(capture); err != nil {
		return consent.Receipt{}, err
	}

	// THE EDITION IS RESOLVED FROM THE SAME READ THE TEXT WAS SERVED FROM
	// (#558). currentPolicyEdition carries the version row's id inside the very
	// entry that carried the notice and the labels to the screen, so the id
	// stamped on this record and the bytes the person saw come from one read of
	// one edition. Reading the current version row again here — which is what
	// this did — would, at a midnight rollover, name edition N+1 as evidence of
	// text N: an acceptance recorded against bytes nobody was shown.
	edition, err := s.currentPolicyEdition(ctx)
	if err != nil {
		return consent.Receipt{}, err
	}

	// The Terms edition is resolved ONLY when the box was shown (#536, ADR
	// 0066), because unlike the Policy Version it is part of the answer's
	// evidence rather than of every act's: a capture that said nothing about the
	// Terms names no edition, and migration 106's CHECK holds answer and edition
	// together. Resolved server-side like the Policy Version and never a
	// parameter — which edition somebody accepted is the platform's finding.
	// A checkout that held its answer across a provider redirect also held the
	// edition it was answered about (#537, migration 108), and the held edition
	// wins: recording the current one instead would claim an acceptance of a
	// text the buyer may never have been shown. Everywhere else the field is
	// empty and the current edition is, correctly, the shown one.
	var termsVersionID string
	if capture.Answers.TermsAcceptance != nil {
		termsVersionID = capture.TermsVersionID
		if termsVersionID == "" {
			// From the cached edition, for the reason above: one read answers
			// both "what was shown" and "what is recorded".
			termsEdition, err := s.currentTermsEdition(ctx)
			if err != nil {
				return consent.Receipt{}, err
			}
			termsVersionID = termsEdition.version.ID
		}
	}

	now := s.now()
	record := repository.Record{
		CustomerID:        capture.CustomerID,
		Email:             capture.Email,
		Channel:           capture.Channel,
		CapturedAt:        now,
		PolicyVersionID:   edition.version.ID,
		PolicyAcceptance:  nullBool(capture.Answers.PolicyAcceptance),
		MarketingConsent:  nullBool(capture.Answers.MarketingConsent),
		NetworkingConsent: nullBool(capture.Answers.NetworkingConsent),
		TermsAcceptance:   nullBool(capture.Answers.TermsAcceptance),
		TermsVersionID:    nullString(termsVersionID),
		// The Adulthood Declaration made in this same act (#586, ADR 0069),
		// written down verbatim and never inferred from the edition's Artifact
		// set. It needs no edition of its own: the box was shown BECAUSE the
		// edition beside it carries the Artifact, so `terms_version_id` already
		// names the exact bytes this person was shown when they declared it.
		//
		// Nothing here refuses a false. The refusal happens at every surface
		// BEFORE this method is reached, which is what makes "a refusal writes no
		// row of any kind" true of the platform rather than of a code path — see
		// consent.ErrAdulthoodDeclarationRequired.
		AdulthoodDeclaration: nullBool(capture.Answers.AdulthoodDeclaration),
		EmailProven:          capture.EmailProven,
		IP:                   nullString(capture.Evidence.IP),
		UserAgent:            nullString(capture.Evidence.UserAgent),
		SessionID:            nullString(capture.Evidence.SessionID),
		OriginURL:            nullString(capture.Evidence.OriginURL),
		// The language of the text that was actually on screen, as the surface
		// that rendered it reports it (#567). Written verbatim and never
		// inferred: this service knows which EDITION it resolved, but an edition
		// publishes several languages and only the renderer knows which one it
		// served — so a channel that showed no document passes nothing and stores
		// NULL, which is the truth about it.
		PresentedLocale: nullString(string(capture.Evidence.PresentedLocale)),
		// Null on every act a Customer performed themselves, which is every
		// channel but the Operator's (#271). Empty becomes SQL NULL rather than
		// the empty string, so "recorded by nobody" has exactly one spelling —
		// which is also what migration 067's CHECK insists on.
		RecordedBy:       nullString(capture.RecordedBy),
		RequestReference: nullString(capture.RequestReference),
	}

	state := repository.StateWrite{
		AcceptPolicy: capture.Answers.PolicyAcceptance != nil && *capture.Answers.PolicyAcceptance,
		// A tick stamps the current edition; anything else — not shown, or shown
		// and refused — leaves the standing state exactly as it was. Nothing can
		// clear it: Terms acceptance has no withdrawal path (ADR 0066).
		AcceptTerms:    capture.Answers.TermsAcceptance != nil && *capture.Answers.TermsAcceptance,
		TermsVersionID: termsVersionID,
		Marketing:      optionalStateWrite(capture.Answers.MarketingConsent, capture.EmailProven),
		Networking:     optionalStateWrite(capture.Answers.NetworkingConsent, capture.EmailProven),
		DigestEnabled:  digestLockstep(capture.Answers.MarketingConsent, capture.EmailProven),
	}

	var recordID string
	var prior, resulting repository.CustomerConsentState
	if tx != nil {
		recordID, prior, resulting, err = s.repo.AppendTx(ctx, tx, record, state)
	} else {
		recordID, prior, resulting, err = s.repo.Append(ctx, record, state)
	}
	if err != nil {
		return consent.Receipt{}, err
	}

	return consent.Receipt{
		RecordID:           recordID,
		CapturedAt:         now,
		PolicyVersionID:    edition.version.ID,
		PolicyVersionLabel: edition.version.Label,
		MarketingConsent:   resulting.MarketingConsent,
		NetworkingConsent:  resulting.NetworkingConsent,
		// What this act TOOK AWAY, decided here and nowhere else (#267). Both
		// halves of the comparison come from the one transaction that observed
		// them under the Customer row's lock — the state as it stood, and the
		// state the same statement made true — so a surface cannot get a
		// different answer by asking again afterwards, and cannot get one at all
		// by inspecting the answers it sent.
		Withdrawn: consent.Withdrawn{
			MarketingConsent:  consent.Withdrew(prior.MarketingConsent, resulting.MarketingConsent),
			NetworkingConsent: consent.Withdrew(prior.NetworkingConsent, resulting.NetworkingConsent),
		},
	}, nil
}

// ConfirmationSent records that the Customer was told about the Consent
// Withdrawal a Consent Record performed (#267, parent #265).
//
// It is deliberately as narrow as a method can be: one record, one timestamp,
// no way to say anything else about it and no way to unsay it. The surface that
// sent the mail calls it AFTER the provider took the message, which is the
// moment the stamp is about — this platform records that it handed the message
// over, not that anybody read it, and there is no delivery fact here to be
// tempted into storing.
//
// A failure to stamp must not fail the withdrawal or the send. The withdrawal
// is the act, the mail is the courtesy that follows it, and this is an
// annotation on evidence that already stands without it.
func (s *Service) ConfirmationSent(ctx context.Context, recordID string, at time.Time) error {
	if recordID == "" {
		return errors.New("consent confirmation sent: no record")
	}
	return s.repo.StampConfirmationSent(ctx, recordID, at)
}

// Outstanding reports which boxes a Customer must still be shown: the predicate
// every capture surface asks before it renders anything, and the one the
// sign-in gate reads to decide whether a Customer Session may be minted.
//
// It reads STATE and never the Consent Record log, which is the rule that keeps
// this cheap enough to sit on the sign-in path: four columns off the Customer
// row and one small lookup of the handful of edition rows, no aggregate over
// history.
//
// THERE ARE NO SPECIAL CASES IN IT, and that is deliberate. A Customer created
// by a box-office sale, one imported from a spreadsheet, one who has been
// signing in since before this feature existed, and one who accepted an
// edition that has since been superseded all fail the same test for the same
// reason: nothing on their row records an acceptance of an edition that still
// satisfies the gate. Nobody is grandfathered, because the acceptance nobody
// recorded cannot be produced later.
//
// THE TEST IS MEMBERSHIP, NOT EQUALITY (#560). It used to compare the stored
// edition id against the single current one; it now asks whether the stored id
// is in the SATISFYING SET — the gating floor and every edition at or above it,
// corrections included (legal.Satisfying). The two agree exactly whenever every
// published edition is gating, which is every environment today, so nothing
// visible changed here. What membership buys is that publishing a CORRECTION
// does not move the floor and so re-gates nobody, for free: no backfill, no
// notification, no column to maintain. Two people holding two different
// editions can both be clear, and each one's evidence still resolves to the
// exact bytes they accepted.
//
// In SQL, over a person row `p`, the two predicates below are:
//
//	policy_version_id IS NULL OR policy_version_id <> ALL($1)
//	terms_version_id  IS NULL OR terms_version_id  <> ALL($1)
func (s *Service) Outstanding(ctx context.Context, customerID string) (consent.Outstanding, error) {
	policyEditions, err := s.repo.SatisfyingPolicyEditions(ctx)
	if errors.Is(err, repository.ErrNoCurrentPolicyVersion) {
		return consent.Outstanding{}, consent.ErrNoCurrentPolicyVersion()
	}
	if err != nil {
		return consent.Outstanding{}, err
	}

	state, err := s.repo.ConsentState(ctx, customerID)
	if errors.Is(err, repository.ErrCustomerNotFound) {
		return consent.Outstanding{}, err
	}
	if err != nil {
		return consent.Outstanding{}, err
	}

	termsEditions, err := s.repo.SatisfyingTermsEditions(ctx)
	if errors.Is(err, repository.ErrNoCurrentTermsVersion) {
		return consent.Outstanding{}, consent.ErrNoCurrentTermsVersion()
	}
	if err != nil {
		return consent.Outstanding{}, err
	}

	// The same version rule over the parallel table (#536, ADR 0066).
	termsOutstanding := !state.TermsVersionID.Valid || !termsEditions.Contains(state.TermsVersionID.String)

	// THE DECLARATION TRACKS THE TERMS BOX AND ASKS THE EDITION FOR THE REST
	// (#586, ADR 0069). It is owed when the acceptance is owed and the edition
	// in effect actually carries the `label-adulthood-declaration` Artifact, and
	// at no other time — there is no standing to compute, no column to read and
	// no second population to chase.
	//
	// The edition is consulted only on the branch that already owes the Terms,
	// which is what keeps this method cheap enough to sit on the sign-in path:
	// somebody Current on the Terms is answered from their own row alone,
	// exactly as before, and only a person who is being stopped anyway pays for
	// the (cached) edition read.
	adulthoodOutstanding := false
	if termsOutstanding {
		edition, err := s.currentTermsEdition(ctx)
		if err != nil {
			return consent.Outstanding{}, err
		}
		adulthoodOutstanding = edition.asksAdulthoodDeclaration()
	}

	return consent.Outstanding{
		// Acceptance is of a VERSION: an acceptance of an edition below the
		// gating floor is not an acceptance of what is in force, which is what
		// makes publishing a gating row re-gate the whole customer base.
		PolicyAcceptance: !state.PolicyVersionID.Valid || !policyEditions.Contains(state.PolicyVersionID.String),
		// Pending Confirmation counts as unanswered here: somebody else's tick is
		// not the owner's answer, and the owner gets asked (ADR 0035).
		MarketingConsent:  !state.MarketingConsent.Answered(),
		NetworkingConsent: !state.NetworkingConsent.Answered(),
		// The same version rule over the parallel table (#536, ADR 0066): the two
		// documents re-gate independently, and nobody is grandfathered here
		// either — on the day the seed lands, every Customer owes edition "1".
		TermsAcceptance: termsOutstanding,
		// And the 18+ box, which is owed only where the acceptance above is
		// (#586). A person Current on an Artifact-carrying edition is therefore
		// never re-asked: they declared it when they accepted, and a refusal
		// would have written nothing at all.
		AdulthoodDeclaration: adulthoodOutstanding,
	}, nil
}

// optionalStateWrite turns one optional answer into the state it writes.
// See Capture's doc comment for the rules; this is only their transcription.
func optionalStateWrite(answer *bool, proven bool) repository.ConsentStateWrite {
	if answer == nil {
		// Not shown: nothing is written, and nothing is cleared.
		return repository.ConsentStateWrite{}
	}
	if proven {
		state := consent.StateDenied
		if *answer {
			state = consent.StateGranted
		}
		return repository.ConsentStateWrite{State: state}
	}
	if !*answer {
		// An unproven No moves NOTHING. It is recorded in the evidence log like
		// every other answer, and it is not allowed to become the platform's
		// belief about a person who has not shown they are that person.
		//
		// Denied would be worse here than pending is for a tick, because denied
		// is ANSWERED: it silences the Follow Digest under the sender's rule, and
		// it stops the real owner from ever being asked again, since Outstanding
		// reads exactly that. A stranger typing a legacy subscriber's address
		// into a checkout and leaving the box alone would have ended their Digest
		// and taken away the prompt that could have restored it.
		//
		// Silence from an unproven party is not a No; it is not an answer at all.
		// The spec's "unticked is an explicit No" is about somebody answering for
		// themselves, which is what the proven branch above does.
		return repository.ConsentStateWrite{}
	}
	return repository.ConsentStateWrite{State: consent.StatePendingConfirmation, OnlyWhenUnanswered: true}
}

// digestLockstep is the Follow Digest flag's half of ADR 0034: one switch, two
// columns, never written apart. nil means leave it alone.
func digestLockstep(marketing *bool, proven bool) *bool {
	if marketing == nil || !proven {
		return nil
	}
	enabled := *marketing
	return &enabled
}

func nullBool(v *bool) sql.NullBool {
	if v == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *v, Valid: true}
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
