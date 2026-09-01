package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/evidence"
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// THE CONSENT EVIDENCE PACK (#568, parent #556, ADR 0067): one file that
// answers a data subject or a regulator, generated from the per-subject record
// and never stored.
//
// THIS IS WHERE IT IS ASSEMBLED, and it has to be here. A pack SPANS BOTH
// POPULATIONS FOR ONE ADDRESS — a person who is both a Customer and somebody
// who signs into the Staff platform gets ONE file — and this service is the
// only layer allowed to hold both at once: consent knows nothing of
// Organizations or operators and must not learn (ADR 0015), identity knows
// nothing of the Policy. The builder below it (consent/evidence) is a pure
// function from a record to bytes, so everything about WHICH rows go in is
// decided here and everything about HOW they are written is decided there.
//
// IT IS GENERATED FROM #566's READ AND FROM NO OTHER. The history is walked
// page by page through CustomerConsentActs with its own cursor — the same read
// the record screen serves — because an export telling a different story from
// the record it was exported from would be worse than no export. There is
// deliberately no "give me every act at once" query beside it: the page size is
// a cap the surface cannot raise precisely so that an evidence log cannot be
// downloaded as a query parameter, and the export is a deliberate, named act.
//
// TWO ENTRY POINTS, ONE FILE. An operator reaches a pack from either record
// screen, and both produce the SAME BYTES for the same human being: the
// customer path resolves the address and asks identity for the staff half, the
// staff path resolves the address and asks consent for the Customer half. Two
// packs for one person that differed by which screen they were generated from
// would be two answers to one access request.
//
// NO SELF-SERVICE. There is no subject-facing route to any of this and there is
// not going to be one. ADR 0039's precedent cuts against it rather than for it:
// a proven email buys a WITHDRAWAL, an act that only ever takes something away,
// where a pack DISCLOSES everything the platform holds. The operator generating
// the pack and replying is the mechanism.

// evidencePackWalkLimit bounds the history walk.
//
// A GUARD AND NOT A CAP. At 25 acts a page this is 25,000 acts for one person,
// which nobody on this platform is within three orders of magnitude of; it
// exists so that a cursor bug cannot turn a download into an unbounded loop
// holding a request open. Reaching it produces the acts read so far rather than
// an error, because a partial record handed over with its own act count is more
// use to a data subject than a 500.
const evidencePackWalkLimit = 1000

// EvidencePack is the finished file on its way to the browser.
type EvidencePack struct {
	Filename    string
	ContentType string
	Body        []byte
	// SHA256 is the pack's fingerprint: what migration 118 stores, what the
	// filename is keyed on, and what #569's access log records for the same
	// export. OFFERED AT THIS SEAM so that the log does not have to recompute
	// it over a copy of the bytes — which would be a second chance for the two
	// to disagree about what was sent.
	SHA256 string
}

// CustomerEvidencePack generates the pack for the Customer with this id.
//
// LEGAL_SUBJECT_NOT_FOUND for an id nobody holds, which is the record read's
// own refusal: a stale link is answered the same way whichever thing the
// operator clicked.
// THE EXPORT IS LOGGED BY NAME AND WITH THE FILE'S OWN FINGERPRINT (#569), and
// the customer id goes on the row because this route was keyed on it — so the
// log can link back to the record the pack came from, and so deleting that
// Customer fails loudly (migration 116's RESTRICT) rather than quietly
// destroying the record of what was disclosed about them.
func (s *Service) CustomerEvidencePack(ctx context.Context, actor, customerID string) (*EvidencePack, error) {
	record, err := s.legalRecords.CustomerLegalRecord(ctx, strings.TrimSpace(customerID))
	if err != nil {
		return nil, err
	}
	pack, err := s.evidencePack(ctx, record.Email, record.ID)
	if err != nil {
		return nil, err
	}
	if err := s.recordEvidenceExport(ctx, actor, record.ID, record.Email, pack.SHA256); err != nil {
		return nil, err
	}
	return pack, nil
}

// StaffEvidencePack generates the pack for the staff person this digest names.
//
// It resolves the digest exactly as the staff record does — by MATCHING across
// the population, never by reversing, because the digest is one-way by design —
// and refuses with 503 on a deployment with no link secret, for the record
// screen's reason: with no key the match would resolve an arbitrary digest to
// the first person in the list, which is not a degraded answer but the wrong
// person's evidence.
// IT IS LOGGED AS A PLAIN ADDRESS AND NEVER AS THE DIGEST (#569) — the digest
// depends on a rotatable key and would orphan the row — and with NO CUSTOMER ID
// even where the cross-link resolves one, because the act was performed against
// the staff record. The `pack_sha256` is the same value the file's name is keyed
// on and the same value migration 118 stores, so the two rows about one handover
// meet on the one fact that cannot be misremembered.
func (s *Service) StaffEvidencePack(ctx context.Context, actor, digest string) (*EvidencePack, error) {
	email, err := s.resolveStaffDigest(ctx, digest)
	if err != nil {
		return nil, err
	}
	// The cross-link, resolved server-side from an address that never leaves it.
	// "" is the ordinary answer: most staff have never bought a ticket.
	customerID, err := s.legalRecords.CustomerIDByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	pack, err := s.evidencePack(ctx, email, customerID)
	if err != nil {
		return nil, err
	}
	if err := s.recordEvidenceExport(ctx, actor, "", email, pack.SHA256); err != nil {
		return nil, err
	}
	return pack, nil
}

// evidencePack builds one pack for one address, from both populations.
//
// GENERATED EVEN WHEN EMPTY. An address the platform holds nothing about
// produces a complete, well-formed file saying so, because "we hold nothing
// about this person" is a provable answer somebody is entitled to receive as a
// document rather than an error an operator has to paraphrase in an email.
func (s *Service) evidencePack(ctx context.Context, email, customerID string) (*EvidencePack, error) {
	input := evidence.Input{Email: email}

	if customerID != "" {
		record, err := s.legalRecords.CustomerLegalRecord(ctx, customerID)
		if err != nil {
			return nil, err
		}
		input.Customer = customerSubject(record)
		acts, err := s.walkConsentActs(ctx, customerID)
		if err != nil {
			return nil, err
		}
		input.Acts = acts
	}

	staff, err := s.legalStaffRecords.StaffLegalRecord(ctx, email)
	if err != nil && !isStaffSubjectNotFound(err) {
		return nil, err
	}
	if staff != nil {
		// The LABELS come from consent, which owns what an edition is called,
		// exactly as the staff record screen resolves them (#566). A second
		// labeller in identity would be a second thing that could disagree with
		// the Legal Center.
		labels, err := s.legalRecords.TermsEditionLabels(ctx)
		if err != nil {
			return nil, err
		}
		input.Staff = staffSubject(staff, labels)
	}

	editions, err := s.editionsFor(ctx, input)
	if err != nil {
		return nil, err
	}
	input.Editions = editions

	pack, err := evidence.Build(input, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	// THE HANDOVER IS RECORDED AND THE PACK IS NOT. Only the SHA-256 and the
	// covered act ids are written (migration 118); the file goes to the
	// operator and nowhere else.
	if _, err := s.legalRecords.RecordEvidencePack(ctx, consentsvc.EvidencePackHandover{
		SHA256:    pack.SHA256,
		SizeBytes: len(pack.Body),
		Acts:      coveredActs(pack),
	}); err != nil {
		return nil, err
	}

	return &EvidencePack{
		Filename:    pack.Filename,
		ContentType: pack.ContentType,
		Body:        pack.Body,
		SHA256:      pack.SHA256,
	}, nil
}

// walkConsentActs reads the WHOLE history through the record's own paged read,
// oldest page last, exactly as the screen accumulates it.
//
// THE PAGE SIZE IS THE RECORD'S, not a bigger one chosen for an export. The cap
// exists so that an evidence log cannot be pulled in one request by anything
// that can set a query parameter, and an export that quietly raised it would
// have reintroduced the thing the cap denies.
func (s *Service) walkConsentActs(ctx context.Context, customerID string) ([]consentsvc.ConsentActItem, error) {
	var (
		acts   []consentsvc.ConsentActItem
		cursor *consentsvc.ConsentActCursor
	)
	for range evidencePackWalkLimit {
		page, err := s.legalRecords.CustomerConsentActs(ctx, consentsvc.CustomerConsentActsQuery{
			CustomerID: customerID,
			After:      cursor,
			Limit:      consentsvc.ConsentRecordPageSize,
		})
		if err != nil {
			return nil, err
		}
		acts = append(acts, page...)
		// A SHORT PAGE IS THE LAST PAGE. The screen asks for one more than the
		// page size to learn whether there is another; here the walk simply
		// continues until a page comes back short, which needs no extra row and
		// no count.
		if len(page) < consentsvc.ConsentRecordPageSize {
			break
		}
		last := page[len(page)-1]
		cursor = &consentsvc.ConsentActCursor{CapturedAt: last.CapturedAt, ID: last.ID}
	}
	return acts, nil
}

// editionsFor resolves every edition either half names to its exact bytes.
//
// COLLECTED FROM THE ACTS AND FROM THE GATES BOTH. The gates matter on their
// own: somebody created before the evidence log existed has a Customer row
// naming an accepted edition and no act naming it, and a pack that took ids
// from the acts alone would print an acceptance with no text behind it.
//
// AN ID THAT RESOLVES TO NOTHING IS DROPPED, not raised. The FK is RESTRICT so
// it should be impossible; if it ever happened, refusing to build would deny
// somebody their record over a referential problem in this platform's own
// tables, and the act still names the id in record.json.
func (s *Service) editionsFor(ctx context.Context, input evidence.Input) ([]evidence.Edition, error) {
	var policyIDs, termsIDs []string
	add := func(ids *[]string, id string) {
		if id != "" && !slices.Contains(*ids, id) {
			*ids = append(*ids, id)
		}
	}
	if input.Customer != nil {
		if ref := input.Customer.Policy.Edition; ref != nil {
			add(&policyIDs, ref.ID)
		}
		if ref := input.Customer.Terms.Edition; ref != nil {
			add(&termsIDs, ref.ID)
		}
	}
	for _, act := range input.Acts {
		add(&policyIDs, act.PolicyEdition.ID)
		if act.TermsEdition != nil {
			add(&termsIDs, act.TermsEdition.ID)
		}
	}
	if input.Staff != nil {
		for _, acceptance := range input.Staff.Acceptances {
			add(&termsIDs, acceptance.TermsEdition.ID)
		}
	}

	policies, err := s.legalRecords.PolicyEditionTexts(ctx, policyIDs)
	if err != nil {
		return nil, err
	}
	terms, err := s.legalRecords.TermsEditionTexts(ctx, termsIDs)
	if err != nil {
		return nil, err
	}

	editions := make([]evidence.Edition, 0, len(policies)+len(terms))
	for _, item := range policies {
		editions = append(editions, editionOf(evidence.DocumentPolicy, item))
	}
	for _, item := range terms {
		editions = append(editions, editionOf(evidence.DocumentTerms, item))
	}
	return editions, nil
}

func editionOf(document string, item consentsvc.LegalEditionTextItem) evidence.Edition {
	return evidence.Edition{
		Document:      document,
		ID:            item.ID,
		Label:         item.Label,
		Gating:        item.Gating,
		EffectiveDate: item.EffectiveDate,
		ContentHash:   item.ContentHash,
		Artifacts:     item.Artifacts,
	}
}

// customerSubject maps the record's landing read into the pack's subject.
//
// THE STANDINGS ARE DROPPED HERE, on purpose. A standing is membership of the
// satisfying set, and which editions are in it moves at midnight as the
// database's own day moves — so a pack carrying one would disagree with itself
// across a night and could not be checked against a stored hash. What survives
// is what was ACCEPTED, which no later publication moves.
func customerSubject(record *consentsvc.CustomerLegalRecordItem) *evidence.Subject {
	return &evidence.Subject{
		ID:        record.ID,
		Email:     record.Email,
		FirstName: record.FirstName,
		LastName:  record.LastName,
		Policy: evidence.Acceptance{
			AcceptedAt: record.Policy.AcceptedAt,
			Edition:    record.Policy.Edition,
		},
		Terms: evidence.Acceptance{
			AcceptedAt: record.Terms.AcceptedAt,
			Edition:    record.Terms.Edition,
		},
		MarketingConsent:  record.MarketingConsent,
		NetworkingConsent: record.NetworkingConsent,
	}
}

// staffSubject maps identity's record into the pack's staff half.
//
// THE STANDING IS DROPPED HERE TOO, and `former` with it: whether somebody is
// still on a membership table today is a fact about today, and this document is
// about acts. The acceptances themselves are every capacity, unfiltered, because
// a pack that filtered would hide evidence of an act the person really
// performed.
func staffSubject(record *identitysvc.StaffLegalRecordItem, labels map[string]string) *evidence.StaffSubject {
	acceptances := make([]evidence.StaffAcceptance, 0, len(record.Acceptances))
	for _, acceptance := range record.Acceptances {
		acceptances = append(acceptances, evidence.StaffAcceptance{
			ID:              acceptance.ID,
			TermsEdition:    consentsvc.LegalEditionRef{ID: acceptance.TermsEditionID, Label: labels[acceptance.TermsEditionID]},
			Capacity:        acceptance.Capacity,
			AcceptedAt:      acceptance.AcceptedAt,
			IP:              acceptance.IP,
			UserAgent:       acceptance.UserAgent,
			SessionID:       acceptance.SessionID,
			OriginURL:       acceptance.OriginURL,
			PresentedLocale: acceptance.PresentedLocale,
			// The declaration rides along unchanged, null included: a pack that
			// dropped it would tell a different story from the record screen,
			// and a pack that defaulted it would tell a false one.
			AdulthoodDeclaration: acceptance.AdulthoodDeclaration,
		})
	}
	return &evidence.StaffSubject{Acceptances: acceptances}
}

// coveredActs is every act the pack disclosed, in migration 118's vocabulary.
func coveredActs(pack evidence.Pack) []consentsvc.EvidencePackAct {
	acts := make([]consentsvc.EvidencePackAct, 0, len(pack.CoveredActs))
	for _, act := range pack.CoveredActs {
		acts = append(acts, consentsvc.EvidencePackAct{Kind: act.Kind, ID: act.ID})
	}
	return acts
}

// isStaffSubjectNotFound reports the ordinary case: this address holds no staff
// acceptance and is on no membership table.
//
// IT IS AN ABSENCE AND NOT A FAILURE, which is the pack's second stated
// limitation made real — the file says "no rows for this address", never "not
// staff", and it says it for a Customer who has never seen the Staff platform
// just as it does for one who has.
func isStaffSubjectNotFound(err error) bool {
	var domain apperror.DomainError
	return errors.As(err, &domain) && domain.Code() == staffSubjectNotFoundCode
}

// staffSubjectNotFoundCode is identity's refusal, matched on the CODE STRING
// because that is the contract apperror deliberately exposes: the concrete type
// is unexported precisely so nothing type-asserts its way past it.
const staffSubjectNotFoundCode = "STAFF_SUBJECT_NOT_FOUND"
