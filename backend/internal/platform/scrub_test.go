package platform_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

func TestScrubTextRedactsWhatNamesAPersonAndKeepsWhatDoesNot(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Emails.
		{"an email", "no customer for buyer@example.com", "no customer for [email]"},
		{"an email in another case", "Buyer.Name+tag@Example.COM refused", "[email] refused"},
		{"an email with accents", "josé.pérez@correo.ec bounced", "[email] bounced"},
		{"an email URL-encoded into a path", "/customers/buyer%40example.com/dossier", "/customers/[email]/dossier"},
		{"an email quoted by a provider", `send to "<ana@example.ec>" failed`, `send to "<[email]>" failed`},

		// Ecuadorian identity numbers: cédula and RUC, by shape.
		{"a cédula", "tax id 1710034065 rejected", "tax id [identity-number] rejected"},
		{"a mistyped cédula, check digit wrong", "tax id 1710034060", "tax id [identity-number]"},
		{"a cédula with a hyphen before its check digit", "cédula 171003406-5", "cédula [identity-number]"},
		{"a natural-person RUC", "RUC 1710034065001", "RUC [identity-number]"},
		{"a company RUC", "RUC 1790012345001.", "RUC [identity-number]."},
		{"a cédula of a citizen registered abroad", "30 prefix 3012345678", "30 prefix [identity-number]"},
		{"a cédula in JSON", `{"number":"0912345678"}`, `{"number":"[identity-number]"}`},
		{"a mobile number, same shape as a cédula", "call 0991234567", "call [identity-number]"},

		// Query strings and fragments.
		{"an absolute URL's query", "GET https://api.example.com/orders?email=x&token=abc failed", "GET https://api.example.com/orders failed"},
		{"a relative URL's query", "calling api/orders?token=secret", "calling api/orders"},
		{"a path's fragment", "redirect to /checkout#step-2 lost", "redirect to /checkout lost"},
		{"a query carrying an email", "https://x.ec/p?to=ana@example.com", "https://x.ec/p"},

		// What must survive untouched.
		{"a UUID", "event 6f1c2a7e-1710-4065-9a3b-171003406500 not found", "event 6f1c2a7e-1710-4065-9a3b-171003406500 not found"},
		{"a 49-digit SRI clave de acceso", "clave 1611202601179001234500120010010000000011234567811 authorised", "clave 1611202601179001234500120010010000000011234567811 authorised"},
		{"a number with no province prefix", "amount 2500000000 cents", "amount 2500000000 cents"},
		{"a nine-digit number", "count 171003406", "count 171003406"},
		{"a cédula-length run inside a word", "ref ABC1710034065", "ref ABC1710034065"},
		{"a cédula-length run inside a longer number", "id 91710034065", "id 91710034065"},
		{"a Postgres error class", "postgres 23505: duplicate key", "postgres 23505: duplicate key"},
		{"a question mark ending a sentence", "is it pending? yes", "is it pending? yes"},
		{"a route pattern", "GET /api/v1/staff/events/{id} answered 500", "GET /api/v1/staff/events/{id} answered 500"},
		{"a recipient already redacted", "ses send otp to [recipient] failed", "ses send otp to [recipient] failed"},
		{"an issue number", "see #761", "see #761"},
		{"nothing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := platform.ScrubText(tt.in); got != tt.want {
				t.Errorf("ScrubText(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestScrubURLDropsEverythingFromTheQueryOn(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://pay.example.com/api/button/V2/Confirm?id=42&clientTxId=abc", "https://pay.example.com/api/button/V2/Confirm"},
		{"/api/v1/customer/session#token", "/api/v1/customer/session"},
		{"?email=ana@example.com", ""},
		{"https://storage.example.com/bucket/ana%40example.com/receipt.pdf", "https://storage.example.com/bucket/[email]/receipt.pdf"},
		{"https://celcer.sri.gob.ec/comprobantes-electronicos-ws/RecepcionComprobantesOffline", "https://celcer.sri.gob.ec/comprobantes-electronicos-ws/RecepcionComprobantesOffline"},
	}
	for _, tt := range tests {
		if got := platform.ScrubURL(tt.in); got != tt.want {
			t.Errorf("ScrubURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScrubErrorScrubsTheTextAndKeepsTheErrorUnderneath(t *testing.T) {
	pgErr := &pgconn.PgError{Severity: "ERROR", Code: "23505", Message: "duplicate key"}
	err := fmt.Errorf("upsert customer ana@example.com (cédula 1710034065): %w", pgErr)

	scrubbed := platform.ScrubError(err)

	if got, want := scrubbed.Error(), "upsert customer [email] (cédula [identity-number]): ERROR: duplicate key (SQLSTATE 23505)"; got != want {
		t.Errorf("ScrubError text = %q, want %q", got, want)
	}
	if !errors.Is(scrubbed, pgErr) {
		t.Error("errors.Is no longer finds the Postgres error under the scrubbed one")
	}
	if got := platform.ErrorClass(scrubbed); got != "postgres 23505" {
		t.Errorf("ErrorClass(scrubbed) = %q, want the class of the error underneath", got)
	}
	if platform.ScrubError(nil) != nil {
		t.Error("ScrubError(nil) is not nil")
	}
}
