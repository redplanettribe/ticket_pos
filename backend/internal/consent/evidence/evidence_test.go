package evidence_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/evidence"
	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Consent Evidence Pack as a pure function from a record to bytes (#568).
//
// Every rule the ticket states that can be checked without a database is
// checked here, and the two that matter most are DETERMINISM and SELF-
// VERIFICATION: a pack that is not byte-identical across days cannot have its
// hash stored instead of itself, and a pack whose fingerprints cannot be
// reproduced from the files beside them prints numbers nobody can check.

// ---- Fixtures ----------------------------------------------------------

func policyEdition() evidence.Edition {
	artifacts := []legal.Artifact{
		{Locale: platform.LocaleES, Slug: "short-notice", Ordinal: 1, Body: "Aviso corto en español."},
		{Locale: platform.LocaleES, Slug: "policy", Ordinal: 2, Body: "# Política\n\nEl texto completo."},
		{Locale: platform.LocaleEN, Slug: "short-notice", Ordinal: 1, Body: "Short notice in English."},
		{Locale: platform.LocaleEN, Slug: "policy", Ordinal: 2, Body: "# Policy\n\nThe whole text."},
	}
	return evidence.Edition{
		Document:      evidence.DocumentPolicy,
		ID:            "11111111-1111-1111-1111-111111111111",
		Label:         "1",
		Gating:        true,
		EffectiveDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ContentHash:   legal.ContentHash(artifacts),
		Artifacts:     artifacts,
	}
}

func termsEdition() evidence.Edition {
	artifacts := []legal.Artifact{
		{Locale: platform.LocaleEN, Slug: "terms", Ordinal: 1, Body: "# Terms\n\nEnglish terms."},
		{Locale: platform.LocaleES, Slug: "terms", Ordinal: 1, Body: "# Términos\n\nTérminos en español."},
	}
	return evidence.Edition{
		Document:      evidence.DocumentTerms,
		ID:            "22222222-2222-2222-2222-222222222222",
		Label:         "1.1",
		Gating:        false,
		EffectiveDate: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
		ContentHash:   legal.ContentHash(artifacts),
		Artifacts:     artifacts,
	}
}

func text(value string) *string { return &value }

func boolean(value bool) *bool { return &value }

func shown(locale string) **string {
	inner := &locale
	return &inner
}

// aFullInput is one human being who is both a Customer and staff, with a
// history that includes a withdrawal.
func aFullInput() evidence.Input {
	policy := policyEdition()
	terms := termsEdition()
	acceptedAt := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	return evidence.Input{
		Email: "someone@example.com",
		Customer: &evidence.Subject{
			ID:        "33333333-3333-3333-3333-333333333333",
			Email:     "someone@example.com",
			FirstName: "Ada",
			LastName:  "Lovelace",
			Policy: evidence.Acceptance{
				AcceptedAt: &acceptedAt,
				Edition:    &consentsvc.LegalEditionRef{ID: policy.ID, Label: policy.Label},
			},
			Terms: evidence.Acceptance{
				AcceptedAt: &acceptedAt,
				Edition:    &consentsvc.LegalEditionRef{ID: terms.ID, Label: terms.Label},
			},
			MarketingConsent:  text("denied"),
			NetworkingConsent: nil,
		},
		Acts: []consentsvc.ConsentActItem{
			{
				// The withdrawal: a row like any other, readable as one because
				// its `prior` value says what it took away.
				ID:                    "44444444-4444-4444-4444-444444444444",
				CapturedAt:            time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
				Channel:               "operator_request",
				Email:                 "someone@example.com",
				PolicyEdition:         consentsvc.LegalEditionRef{ID: policy.ID, Label: policy.Label},
				MarketingConsent:      boolean(false),
				PriorMarketingConsent: text("granted"),
				EmailProven:           true,
				RecordedBy:            text("operator@example.com"),
				RequestReference:      text("ticket 4711"),
			},
			{
				ID:                "55555555-5555-5555-5555-555555555555",
				CapturedAt:        acceptedAt,
				Channel:           "checkout",
				Email:             "someone@example.com",
				PolicyEdition:     consentsvc.LegalEditionRef{ID: policy.ID, Label: policy.Label},
				PolicyAcceptance:  boolean(true),
				MarketingConsent:  boolean(true),
				NetworkingConsent: nil,
				TermsAcceptance:   boolean(true),
				TermsEdition:      &consentsvc.LegalEditionRef{ID: terms.ID, Label: terms.Label},
				EmailProven:       true,
				IP:                text("203.0.113.9"),
				UserAgent:         text("Mozilla/5.0 (X11; Linux x86_64)"),
				PresentedLocale:   shown("es"),
			},
		},
		Staff: &evidence.StaffSubject{
			Acceptances: []evidence.StaffAcceptance{{
				ID:              "66666666-6666-6666-6666-666666666666",
				TermsEdition:    consentsvc.LegalEditionRef{ID: terms.ID, Label: terms.Label},
				Capacity:        "organizer",
				AcceptedAt:      time.Date(2026, 3, 4, 8, 30, 0, 0, time.UTC),
				IP:              text("198.51.100.4"),
				PresentedLocale: text("en"),
			}},
		},
		Editions: []evidence.Edition{policy, terms},
	}
}

// ---- Helpers -----------------------------------------------------------

func build(t *testing.T, in evidence.Input, on time.Time) evidence.Pack {
	t.Helper()
	pack, err := evidence.Build(in, on)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return pack
}

// entries reads the archive back as an ordered list of names and a map of
// bodies.
func entries(t *testing.T, body []byte) ([]string, map[string][]byte) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	names := make([]string, 0, len(reader.File))
	bodies := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
		opened, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		bodies[file.Name] = content
	}
	return names, bodies
}

// record is record.json parsed loosely, so the test reads the file the way a
// regulator's script would rather than through the package's own structs.
func record(t *testing.T, bodies map[string][]byte) map[string]any {
	t.Helper()
	raw, ok := bodies[evidence.RecordEntry]
	if !ok {
		t.Fatalf("no %s in the pack", evidence.RecordEntry)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse %s: %v", evidence.RecordEntry, err)
	}
	return parsed
}

// ---- The rulings -------------------------------------------------------

// TestTwoPacksOverOneRecordAreByteIdentical is the ruling the whole design
// rests on: it is what allows migration 118 to store a hash instead of a file.
//
// GENERATED ON DIFFERENT DAYS, which is the case that would fail if a clock
// reached the bytes — through a ZIP entry timestamp, a PDF CreationDate, or a
// "generated_at" somebody added to record.json meaning to be helpful.
func TestTwoPacksOverOneRecordAreByteIdentical(t *testing.T) {
	monday := build(t, aFullInput(), time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC))
	// A year later, a different hour, a different zone.
	later := build(t, aFullInput(), time.Date(2027, 9, 30, 23, 45, 0, 0, time.FixedZone("x", 3600)))

	if !bytes.Equal(monday.Body, later.Body) {
		t.Fatalf("two packs over one record differ: %d bytes vs %d", len(monday.Body), len(later.Body))
	}
	if monday.SHA256 != later.SHA256 {
		t.Fatalf("SHA-256 differs: %s vs %s", monday.SHA256, later.SHA256)
	}
	// The FILENAME is the one thing that may differ, and it carries the date on
	// purpose: a filename is a label on a document, not the document.
	if monday.Filename == later.Filename {
		t.Fatalf("both packs are called %q; the filename carries the generation date", monday.Filename)
	}
}

// TestTheFilenameIsKeyedOnThePacksOwnHash pins the #546-vs-#548 resolution
// (ADR 0067). The name is not the email, is not derived from any rotatable key,
// and is checkable against the file itself.
func TestTheFilenameIsKeyedOnThePacksOwnHash(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))

	want := "consent-evidence-" + pack.SHA256[:16] + "-2026-04-06.zip"
	if pack.Filename != want {
		t.Fatalf("Filename = %q; want %q", pack.Filename, want)
	}
	sum := sha256.Sum256(pack.Body)
	if hex.EncodeToString(sum[:]) != pack.SHA256 {
		t.Fatalf("the reported SHA-256 is not the SHA-256 of the body")
	}
	if strings.Contains(pack.Filename, "someone") || strings.Contains(pack.Filename, "example.com") {
		t.Fatalf("the filename names the subject: %q", pack.Filename)
	}
}

// TestThePackHoldsTheThreeThingsItPromises walks the archive's shape: the
// record, its reading, and the texts under their locale-and-ordinal names.
func TestThePackHoldsTheThreeThingsItPromises(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	names, bodies := entries(t, pack.Body)

	// FIXED ENTRY ORDERING: the two documents first, then the texts by path.
	if names[0] != evidence.RecordEntry || names[1] != evidence.EvidenceEntry {
		t.Fatalf("entries start %v; want %s then %s", names[:2], evidence.RecordEntry, evidence.EvidenceEntry)
	}
	texts := names[2:]
	if !slices.IsSorted(texts) {
		t.Fatalf("the text entries are not in path order: %v", texts)
	}
	want := []string{
		"texts/policy/1/en/1-short-notice.md",
		"texts/policy/1/en/2-policy.md",
		"texts/policy/1/es/1-short-notice.md",
		"texts/policy/1/es/2-policy.md",
		"texts/terms/1.1/en/1-terms.md",
		"texts/terms/1.1/es/1-terms.md",
	}
	if !slices.Equal(texts, want) {
		t.Fatalf("text entries = %v; want %v", texts, want)
	}
	// RAW BYTES, not JSON strings and not a re-encoding: these are the bytes
	// that were hashed.
	if got := string(bodies["texts/policy/1/en/2-policy.md"]); got != "# Policy\n\nThe whole text." {
		t.Fatalf("the stored text was rewritten on the way in: %q", got)
	}
	if len(bodies[evidence.EvidenceEntry]) == 0 || !bytes.HasPrefix(bodies[evidence.EvidenceEntry], []byte("%PDF")) {
		t.Fatalf("evidence.pdf is not a PDF")
	}
}

// TestEveryLocaleOfEveryEditionRides is the ruling that a single-locale pack
// could not verify itself: the fingerprint spans all languages at once, so the
// pack carries all of them however few the person was shown.
func TestEveryLocaleOfEveryEditionRides(t *testing.T) {
	in := aFullInput()
	// The person was only ever shown Spanish.
	in.Acts[1].PresentedLocale = shown("es")

	pack := build(t, in, time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	names, _ := entries(t, pack.Body)

	for _, want := range []string{"texts/policy/1/en/1-short-notice.md", "texts/terms/1.1/en/1-terms.md"} {
		if !slices.Contains(names, want) {
			t.Fatalf("%s is missing; the pack could not verify its own hash without it", want)
		}
	}
}

// TestTextsAreDeduplicatedByEditionNotByAct: twenty acts against one edition
// carry that edition's text once. It is what bounds a pack's size by how many
// editions exist rather than by how active somebody was.
func TestTextsAreDeduplicatedByEditionNotByAct(t *testing.T) {
	in := aFullInput()
	busy := in.Acts[1]
	for i := range 20 {
		busy.ID = fmt.Sprintf("aaaaaaaa-0000-0000-0000-%012d", i)
		in.Acts = append(in.Acts, busy)
	}

	pack := build(t, in, time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	names, _ := entries(t, pack.Body)

	if got := len(names) - 2; got != 6 {
		t.Fatalf("%d text entries for 22 acts against 2 editions; want 6", got)
	}
}

// TestThePackVerifiesItsOwnHash is the self-verification property, performed
// exactly as a reader with a shell would: take record.json's preimage list,
// frame each field from the FILES IN THE ARCHIVE, and reproduce content_hash.
//
// It reimplements the framing here on purpose rather than calling
// legal.ContentHash: a test that asked the implementation to check itself would
// pass however wrong the rule the pack printed was.
func TestThePackVerifiesItsOwnHash(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	_, bodies := entries(t, pack.Body)
	parsed := record(t, bodies)

	editions, _ := parsed["editions"].([]any)
	if len(editions) != 2 {
		t.Fatalf("record.json names %d editions; want 2", len(editions))
	}
	for _, raw := range editions {
		edition, _ := raw.(map[string]any)
		sum := sha256.New()
		for _, rawField := range edition["preimage"].([]any) {
			field, _ := rawField.(map[string]any)
			var value []byte
			switch field["field"] {
			case "locale":
				value = []byte(field["locale"].(string))
			case "artifact":
				path, _ := field["path"].(string)
				body, ok := bodies[path]
				if !ok {
					t.Fatalf("the preimage names %q, which is not in the pack", path)
				}
				value = body
			default:
				t.Fatalf("unknown preimage field kind %v", field["field"])
			}
			if got := int(field["bytes"].(float64)); got != len(value) {
				t.Fatalf("preimage says %d bytes, the file is %d", got, len(value))
			}
			fmt.Fprintf(sum, "%d\n%s", len(value), value)
		}
		if got := hex.EncodeToString(sum.Sum(nil)); got != edition["content_hash"] {
			t.Fatalf("edition %v: reproduced %s, record.json says %v",
				edition["label"], got, edition["content_hash"])
		}
	}
}

// TestAnEditionStatesHowItWasPublishedAndNeverWhetherItCurrentlyGates is the
// determinism ruling stated as a shape: `published_as` was fixed at publication
// and may travel; standing changes at midnight and may not.
func TestAnEditionStatesHowItWasPublishedAndNeverWhetherItCurrentlyGates(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	_, bodies := entries(t, pack.Body)
	parsed := record(t, bodies)

	published := map[string]string{}
	for _, raw := range parsed["editions"].([]any) {
		edition, _ := raw.(map[string]any)
		published[edition["label"].(string)] = edition["published_as"].(string)
		for _, forbidden := range []string{"standing", "current", "satisfying", "is_current", "gating_now"} {
			if _, present := edition[forbidden]; present {
				t.Fatalf("an edition carries the time-varying field %q", forbidden)
			}
		}
	}
	if published["1"] != "gating" || published["1.1"] != "correction" {
		t.Fatalf("published_as = %v; want 1 gating and 1.1 correction", published)
	}
}

// TestAnEmptyPackIsAFileAndNotAnError: "we hold nothing about this person" is
// an answer somebody is entitled to receive as a document.
func TestAnEmptyPackIsAFileAndNotAnError(t *testing.T) {
	pack := build(t, evidence.Input{Email: "nobody@example.com"}, time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))

	names, bodies := entries(t, pack.Body)
	if len(names) != 2 {
		t.Fatalf("an empty pack holds %v; want just the record and its reading", names)
	}
	parsed := record(t, bodies)
	if parsed["customer"] != nil {
		t.Fatalf("customer = %v; want null", parsed["customer"])
	}
	if parsed["staff"] != nil {
		t.Fatalf("staff = %v; want null", parsed["staff"])
	}
	// An empty ARRAY and not a null: "performed no acts" is a different
	// sentence from "this field was not filled in".
	acts, ok := parsed["customer_acts"].([]any)
	if !ok || len(acts) != 0 {
		t.Fatalf("customer_acts = %v; want []", parsed["customer_acts"])
	}
	if len(pack.CoveredActs) != 0 {
		t.Fatalf("an empty pack covers %v", pack.CoveredActs)
	}
	// The caveats are still stated: an empty pack is exactly the one a reader
	// is most likely to over-read.
	assertStatesItsLimitations(t, bodies)
}

// TestWithdrawalsAreTheActsThemselves: no synthesised log, and the act is
// readable as a withdrawal because it carries what the consent WAS.
func TestWithdrawalsAreTheActsThemselves(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	_, bodies := entries(t, pack.Body)
	parsed := record(t, bodies)

	for _, forbidden := range []string{"withdrawals", "withdrawal_log", "consent_history"} {
		if _, present := parsed[forbidden]; present {
			t.Fatalf("record.json carries a synthesised %q section", forbidden)
		}
	}
	first, _ := parsed["customer_acts"].([]any)[0].(map[string]any)
	if first["marketing_consent"] != false || first["prior_marketing_consent"] != "granted" {
		t.Fatalf("the withdrawal act does not read as one: %v", first)
	}
	// NULL KEEPS MEANING "NOT SHOWN": the operator-recorded withdrawal answered
	// no policy box and shows no document, so one field is null and the other
	// is absent entirely.
	if first["policy_acceptance"] != nil {
		t.Fatalf("policy_acceptance = %v; want null on a surface that did not show the box", first["policy_acceptance"])
	}
	if _, present := first["presented_locale"]; present {
		t.Fatalf("presented_locale is present on a channel that shows no document")
	}
}

// hexToken is a run of lowercase hex, which is how a Staff Digest would appear
// if one ever leaked into the file: 32 characters exactly.
var hexToken = regexp.MustCompile(`[0-9a-f]+`)

// TestTheHMACDigestAppearsNowhereInTheContents (#548). The guarantee is
// structural — this package holds no digester and cannot mint one — and this
// asserts the shape as well, so a later change that passed one in through a
// field has to argue with a test.
func TestTheHMACDigestAppearsNowhereInTheContents(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	_, bodies := entries(t, pack.Body)

	raw := string(bodies[evidence.RecordEntry])
	if strings.Contains(raw, "digest") {
		t.Fatalf("record.json mentions a digest")
	}
	for _, token := range hexToken.FindAllString(raw, -1) {
		if evidence.ContainsDigest(token) {
			t.Fatalf("record.json carries a 32-hex token that could be a Staff Digest: %q", token)
		}
	}
}

// TestThePackStatesWhatMakesAConsentValidAndWhatItDoesNotSay is the three
// paragraphs #568 requires, in both files.
func TestThePackStatesWhatMakesAConsentValidAndWhatItDoesNotSay(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))
	_, bodies := entries(t, pack.Body)
	assertStatesItsLimitations(t, bodies)
}

func assertStatesItsLimitations(t *testing.T, bodies map[string][]byte) {
	t.Helper()
	raw := string(bodies[evidence.RecordEntry])
	if !strings.Contains(raw, "email_proven") {
		t.Fatalf("record.json never says what makes a consent valid")
	}
	for _, phrase := range []string{
		"NO ROWS FOR THIS ADDRESS",
		"ASSERTED AT THE MOMENT OF CAPTURE",
	} {
		if !strings.Contains(raw, phrase) {
			t.Fatalf("record.json does not state the limitation %q", phrase)
		}
	}
}

// TestTheCoveredActsSpanBothPopulations: one address, one file, and the
// coverage rows migration 118 stores name every act in it.
func TestTheCoveredActsSpanBothPopulations(t *testing.T) {
	pack := build(t, aFullInput(), time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC))

	kinds := map[string]int{}
	for _, act := range pack.CoveredActs {
		kinds[act.Kind]++
	}
	if kinds[evidence.ActKindConsentRecord] != 2 || kinds[evidence.ActKindStaffTermsAcceptance] != 1 {
		t.Fatalf("covered acts = %v; want 2 consent records and 1 staff acceptance", kinds)
	}
}

// TestOneEditionSuppliedTwiceIsRefused. The editions are deduplicated by the
// caller; two rows for one edition would put two copies of one text in the
// archive under one path, and a ZIP with duplicate names is a file readers
// disagree about.
func TestOneEditionSuppliedTwiceIsRefused(t *testing.T) {
	in := aFullInput()
	in.Editions = append(in.Editions, policyEdition())

	if _, err := evidence.Build(in, time.Now()); err == nil {
		t.Fatalf("Build accepted the same edition twice")
	}
}
