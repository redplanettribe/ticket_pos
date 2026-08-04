package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Follow intent through sign-in (#219, parent #215).
//
// The Follow control is drawn for anonymous visitors, and the surfaces it lives
// on are overwhelmingly anonymous. So the moment somebody decides to Follow
// something and the moment they can prove who they are lie a mailbox apart, and
// the decision has to survive the gap. It travels as ONE STRING, from the query
// parameter on the sign-in address through to the verification request, and it
// is parsed exactly once — here.
//
// Two properties are the whole design, and both are about what this string is
// NOT allowed to be.
//
// It names a SUBJECT and never a subscriber. There is no email in a FollowIntent
// and no way to spell one: whose Follow it is comes from the Customer Session
// that verification produced, and from nothing a request can say. Were it
// otherwise, this door would be a way to subscribe an address the caller does
// not control to a weekly email — exactly what ADR 0010 forbids and what the
// Follow Digest (ADR 0030) would then act on, week after week.
//
// It is explicit and server-visible, rather than stashed in browser storage. A
// value the server never sees is a value the server cannot validate, and this
// one is validated to the letter: a known kind of subject, an identifier shaped
// like the slug it claims to be, and nothing that could be a URL, a path, or an
// address. That is also why it is not a redirect: the destination a visitor
// returns to is the existing `next` and is guarded where it always was; an
// intent decides what gets Followed and can steer nobody anywhere.

// FollowSubjectKind names a kind of thing a Follow intent may be about.
//
// The set is closed on purpose and is checked against by exact match, so an
// unrecognised kind is a refusal rather than a guess. Tag Follows (#218) add
// their own member here and change nothing else about this file.
type FollowSubjectKind string

// FollowSubjectKindOrganization is the only kind today, and it is spelled the
// same as the `type` discriminator an Organization Follow carries on the wire
// (FollowSubjectOrganization) — deliberately, so an intent and the Follow it
// produces read as the same fact.
const FollowSubjectKindOrganization FollowSubjectKind = FollowSubjectOrganization

// FollowIntent is a Follow somebody asked for before they could be asked who
// they are.
//
// Nothing in it says who: that is the point. A parsed FollowIntent is inert — it
// becomes a Follow only when handed a Customer Session, and only for the
// Customer that session belongs to.
type FollowIntent struct {
	// Kind is the sort of subject, already checked against the closed set.
	Kind FollowSubjectKind
	// Key identifies the subject within its kind: an Organization's slug today,
	// a Tag's canonical key when Tag Follows land. Already normalised.
	Key string
}

// followIntentSeparator divides the kind from the key. One colon, and exactly
// one: a second is a second field somebody is trying to smuggle in, and it is
// refused rather than ignored.
const followIntentSeparator = ":"

// followIntentField is the name a refused intent is reported under, and it is
// the name the value travels under everywhere: the `follow` query parameter on
// the sign-in address and the `follow` field on the verification request.
const followIntentField = "follow"

// followIntentKeyPattern is the shape of an identifier a Follow intent may name:
// the Organization slug pattern the catalog and identity handlers already
// enforce on the way in, restated here on the way back out.
//
// It is doing more work than a format check. It admits no "/", no ":", no ".",
// no "@" and no whitespace, which is what makes it impossible to spell a URL, a
// protocol-relative host, a parent-directory traversal or an email address in
// the subject position — the four things a caller-supplied identifier destined
// for a lookup would otherwise be tried as.
var followIntentKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// followIntentKeyMaxLength bounds the identifier. No slug this platform issues
// comes near it; the cap exists so an intent cannot be used to hand the database
// an unbounded string to compare.
const followIntentKeyMaxLength = 100

// followIntentKinds is the closed set, in the wording a refusal names it by.
const followIntentKinds = `"organization"`

// ParseFollowIntent turns the string an intent travels as into a FollowIntent,
// or reports that it is not one.
//
// An empty string is not an error — it is the ordinary sign-in, which carries no
// intent at all — and comes back as a nil intent with a nil error. Everything
// else must be a known kind, a separator, and a well-formed key, or it is
// refused: this is caller-supplied text arriving at a write, and "nearly" is not
// a shape anything here accepts.
//
// The key is required to be canonical rather than merely canonicalisable. Every
// intent this platform emits is built from a slug the API itself published, so
// there is no reader for whom "TEST-ORG" is the honest spelling of anything, and
// accepting variants would mean the string a person can see in their address bar
// is not the string that was acted on.
// It reports its refusals as field errors rather than as a domain error because
// they are exactly that: a request that was written wrong, answered with 400 and
// a field to fix, in the same envelope every other malformed field on these
// routes already uses. The codes are the existing shared ones — the rule that
// failed is "not one of the accepted values" or "not URL-safe", and those rules
// mean the same thing here as anywhere else (platform/validation_codes.go).
func ParseFollowIntent(raw string) (*FollowIntent, []platform.FieldError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	unknownKind := []platform.FieldError{{
		Field:   followIntentField,
		Code:    platform.CodeInvalidEnum,
		Message: fmt.Sprintf("must name a kind of Follow (%s) and its subject, like organization:my-org", followIntentKinds),
	}}

	kind, key, found := strings.Cut(raw, followIntentSeparator)
	// A missing separator, or a second one, is refused as a kind failure rather
	// than parsed generously. "organization:my-org:tag:techno" is somebody
	// appending a field this format does not have, and the honest answer is that
	// the whole string is not an intent.
	if !found || strings.Contains(key, followIntentSeparator) {
		return nil, unknownKind
	}
	if FollowSubjectKind(kind) != FollowSubjectKindOrganization {
		return nil, unknownKind
	}
	if len(key) > followIntentKeyMaxLength {
		return nil, []platform.FieldError{{
			Field:   followIntentField,
			Code:    platform.CodeTooLong,
			Message: fmt.Sprintf("subject must be at most %d characters", followIntentKeyMaxLength),
		}}
	}
	if !followIntentKeyPattern.MatchString(key) {
		return nil, []platform.FieldError{{
			Field:   followIntentField,
			Code:    platform.CodeInvalidSlug,
			Message: "subject must be URL-safe (lowercase letters, numbers, and hyphens)",
		}}
	}
	return &FollowIntent{Kind: FollowSubjectKind(kind), Key: key}, nil
}

// ApplyFollowIntent records the Follow somebody asked for before signing in,
// against the Customer Session that signing in just produced.
//
// `token` is always a session this process minted moments ago from a completed
// Proof of Email Ownership, and it is the ONLY thing that decides whose Follow
// this becomes. It reaches the write through FollowOrganization — the same
// function the ordinary Follow endpoint calls — so every rule that governs a
// Follow governs this one too, and none of them is restated here where the two
// could drift: the full-session gate, the idempotent repeat, the slug that must
// resolve.
//
// A failure is swallowed and reported as "no Follow" rather than as an error.
// Two things were asked for in one request and only one of them is the reason
// the person is here: they came to sign in. An Organization deleted while they
// were in their mailbox, or a database that will not take the write, must not
// cost them the session they proved they were entitled to — they land signed in,
// with the control simply not showing Followed, which is a state they can fix
// with one press. The refusal is logged so it is not silent to us.
func (s *Service) ApplyFollowIntent(ctx context.Context, token string, intent *FollowIntent) *FollowView {
	if intent == nil {
		return nil
	}
	switch intent.Kind {
	case FollowSubjectKindOrganization:
		follow, err := s.FollowOrganization(ctx, token, intent.Key)
		if err != nil {
			s.logger.Info("follow intent not applied", "kind", string(intent.Kind), "error", err.Error())
			return nil
		}
		return follow
	default:
		// Unreachable: ParseFollowIntent is the only source of a FollowIntent and
		// refuses every kind this switch does not handle. Stated anyway, so that
		// adding a kind to the set without adding it here follows no Follow
		// rather than following the wrong thing.
		return nil
	}
}
