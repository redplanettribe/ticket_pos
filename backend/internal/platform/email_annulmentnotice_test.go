package platform

import (
	"strings"
	"testing"
)

// The Annulment Notice (#748, spec #742, ADR 0078): the buyer of a Sale
// Invoice is told the factura they were sent was annulled at the SRI. These
// tests assert on the RENDERED words, in both languages and both of the
// Sale's states, as the Tax Document delivery's do.

func standingNotice(locale Locale) AnnulmentNotice {
	return AnnulmentNotice{
		To:              "ana@example.com",
		CustomerName:    "Ana Lopez",
		EventName:       "Noche de Jazz",
		Reference:       "ABC123",
		Number:          "001-001-000000007",
		CustomerAreaURL: "https://example.test/tickets#sale-1",
		Locale:          locale,
	}
}

func wantAll(t *testing.T, what, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Fatalf("%s: text = %q, want it to contain %q", what, text, w)
		}
	}
}

func wantNone(t *testing.T, what, text string, banned ...string) {
	t.Helper()
	for _, b := range banned {
		if strings.Contains(text, b) {
			t.Fatalf("%s: text = %q, says %q", what, text, b)
		}
	}
}

func TestAnnulmentNoticeForAStandingSaleInEnglish(t *testing.T) {
	n := standingNotice(LocaleEN)
	if got := n.Subject(); got != "Your tax invoice (factura) for Noche de Jazz was annulled at the SRI" {
		t.Fatalf("subject = %q", got)
	}
	text := n.Text()
	wantAll(t, "standing en", text,
		"Hi Ana Lopez,",
		"The tax invoice (factura) 001-001-000000007 for your purchase for Noche de Jazz was annulled at the SRI.",
		"It has no tax effect: please discard it",
		"Reference: ABC123",
		"Your purchase and your tickets are unchanged.",
		"A new tax invoice (factura) will follow by email.",
		"after signing in:\nhttps://example.test/tickets#sale-1",
	)
	wantNone(t, "standing en", text, "Nothing else follows")
}

// The standing wording's "purchase unchanged" line is load-bearing in
// Spanish: the Sale Voided mail says "Su compra ... fue anulada", and a
// buyer must never read this one as that.
func TestAnnulmentNoticeForAStandingSaleInSpanish(t *testing.T) {
	n := standingNotice(LocaleES)
	if got := n.Subject(); got != "Su factura de Noche de Jazz fue anulada en el SRI" {
		t.Fatalf("subject = %q", got)
	}
	text := n.Text()
	wantAll(t, "standing es", text,
		"Hola Ana Lopez:",
		"La factura 001-001-000000007 de su compra de Noche de Jazz fue anulada en el SRI.",
		"No tiene efecto tributario: descártela",
		"Referencia: ABC123",
		"Su compra y sus entradas no cambian.",
		"Recibirá una nueva factura por correo.",
		"después de iniciar sesión:\nhttps://example.test/tickets#sale-1",
	)
	wantNone(t, "standing es", text, "Su compra fue anulada", "compra anulada", "Hi ", "No se enviará")
}

func TestAnnulmentNoticeForAReversedSaleSaysNothingFollows(t *testing.T) {
	for locale, want := range map[Locale][2]string{
		LocaleEN: {"Nothing else follows: no new tax invoice (factura) will be sent.", "will follow by email"},
		LocaleES: {"No se enviará nada más: no habrá una nueva factura.", "Recibirá una nueva factura"},
	} {
		n := standingNotice(locale)
		n.SaleReversed = true
		text := n.Text()
		wantAll(t, string(locale)+" reversed", text, want[0])
		wantNone(t, string(locale)+" reversed", text, want[1], "unchanged", "no cambian")
	}
}

func TestAnnulmentNoticeWithoutAStorefrontOffersNoLink(t *testing.T) {
	n := standingNotice(LocaleEN)
	n.CustomerAreaURL = ""
	wantNone(t, "no link", n.Text(), "signing in", "http")
}
