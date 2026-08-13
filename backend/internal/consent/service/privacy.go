package service

import (
	"context"
	"errors"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
)

// The Privacy page's read (#268, parent #265).
//
// THIS FILE WRITES NOTHING, and that is its acceptance criterion rather than an
// incidental property. Rendering a settings page must leave the platform
// exactly as it was: a page that recorded a refusal because somebody read it
// would turn "never asked" into "denied" for everybody who opened it and did
// nothing, which is both a lie about them and an act they never performed. The
// only way to move a consent stays the one write path (capture.go), reached by
// moving a control.
//
// Nothing else about the Privacy page is here. What a control DOES when it is
// moved is an ordinary Capture on the `account_settings` channel, performed by
// the customers module's surface exactly as the Digest toggle already performs
// one — because a withdrawal is a capture act and not a mechanism of its own
// (ADR 0038), and a "withdraw" method here would be the second write path that
// ADR exists to refuse.

// PrivacyState reports what one Customer has authorized: the Policy Version
// they accepted and when, and the current state of each optional consent.
//
// IT READS THE CUSTOMER ROW AND NEVER THE CONSENT RECORD LOG, the same rule
// Outstanding follows and for the same reason: the log says what happened and
// the state says what is true now, and a settings page is a question about now.
// It is also what keeps this cheap — four columns off one row, plus a label
// lookup only for a Customer who has actually accepted something.
//
// A MISSING EDITION IS NOT A FAILED PAGE. The label lookup is by a foreign key
// with ON DELETE RESTRICT, so it cannot miss; if it ever does, the row is
// logged and the page still reports both consents truthfully rather than
// refusing to tell somebody about their own rights over a version label.
func (s *Service) PrivacyState(ctx context.Context, customerID string) (consent.PrivacyState, error) {
	if customerID == "" {
		return consent.PrivacyState{}, errors.New("consent privacy state: no customer")
	}

	state, err := s.repo.ConsentState(ctx, customerID)
	if err != nil {
		return consent.PrivacyState{}, err
	}

	privacy := consent.PrivacyState{
		// Copied across as they stand, INCLUDING THE EMPTY STRING. Never answered
		// is not a refusal, and nothing here may substitute one for the other.
		MarketingConsent:  state.MarketingConsent,
		NetworkingConsent: state.NetworkingConsent,
	}
	if state.PolicyAcceptedAt.Valid {
		privacy.PolicyAcceptedAt = state.PolicyAcceptedAt.Time
	}
	if !state.PolicyVersionID.Valid {
		// No acceptance recorded at all. Reachable for a Customer signing in on a
		// session minted before consent capture existed, and the page says so
		// rather than naming an edition they never saw.
		return privacy, nil
	}

	version, err := s.repo.PolicyVersionByID(ctx, state.PolicyVersionID.String)
	if errors.Is(err, repository.ErrPolicyVersionNotFound) {
		s.logger.Error("a Customer's accepted policy version is not in policy_versions",
			"customer_id", customerID,
			"policy_version_id", state.PolicyVersionID.String,
		)
		return privacy, nil
	}
	if err != nil {
		return consent.PrivacyState{}, err
	}
	privacy.PolicyVersionLabel = version.Label
	return privacy, nil
}
