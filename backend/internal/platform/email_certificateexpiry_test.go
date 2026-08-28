package platform

import (
	"strings"
	"testing"
	"time"
)

// The Certificate Expiry Warning (#502, ADR 0063) is the platform's first
// mail about its own machinery rather than about a sale, and these tests
// assert on the RENDERED words as every other message's do: the four
// thresholds are four different subjects, and a test that only checked a
// Threshold was carried would pass just as happily against copy that never
// changed with it.

// aCertificateExpiryWarning is a warning about a certificate whose NotAfter is
// 03:00 UTC on 28 September — which is the evening of the 27th in Guayaquil.
// The date the reader must be told is the 27th: the whole job of the date is
// to let them count days, and a mail naming the UTC day would be off by one
// for the whole of the last evening.
func aCertificateExpiryWarning(threshold int, locale Locale) CertificateExpiryWarning {
	return CertificateExpiryWarning{
		To:        "ops@example.com",
		Locale:    locale,
		Threshold: threshold,
		NotAfter:  time.Date(2026, time.September, 28, 3, 0, 0, 0, time.UTC),
		RUC:       "1790012345001",
		IssuerURL: "https://staff.example.test/operator/invoicing/issuer",
	}
}

func TestCertificateExpiryWarningSubjectNamesEachThresholdInEnglish(t *testing.T) {
	for threshold, want := range map[int]string{
		30: "Signing certificate expires in 30 days",
		7:  "Signing certificate expires in 7 days",
		1:  "Signing certificate expires tomorrow",
		0:  "Signing certificate has expired",
	} {
		if got := aCertificateExpiryWarning(threshold, LocaleEN).Subject(); got != want {
			t.Errorf("threshold %d: subject = %q, want %q", threshold, got, want)
		}
	}
}

func TestCertificateExpiryWarningSubjectNamesEachThresholdInSpanish(t *testing.T) {
	for threshold, want := range map[int]string{
		30: "El certificado de firma vence en 30 días",
		7:  "El certificado de firma vence en 7 días",
		1:  "El certificado de firma vence mañana",
		0:  "El certificado de firma ha vencido",
	} {
		if got := aCertificateExpiryWarning(threshold, LocaleES).Subject(); got != want {
			t.Errorf("threshold %d: subject = %q, want %q", threshold, got, want)
		}
	}
}

func TestCertificateExpiryWarningIsWrittenInEnglishWhenNothingNamedALanguage(t *testing.T) {
	w := aCertificateExpiryWarning(7, "")

	if got := w.Subject(); got != "Signing certificate expires in 7 days" {
		t.Fatalf("subject = %q, want the English subject", got)
	}
	text := w.Text()
	for _, want := range []string{
		"The signing certificate of the Issuer with RUC 1790012345001 expires on 27 September 2026 (Ecuador time).",
		"Once it lapses, every Sale Invoice owed is parked unsigned — no sequential number, no submission — while the SRI's 24-hour window for transmitting each one keeps running.",
		"Upload the renewed .p12 on the Issuer page:\nhttps://staff.example.test/operator/invoicing/issuer",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
}

func TestCertificateExpiryWarningIsWrittenInSpanishForASpanishReader(t *testing.T) {
	w := aCertificateExpiryWarning(30, LocaleES)

	text := w.Text()
	for _, want := range []string{
		"Aviso de vencimiento del certificado",
		"El certificado de firma del Emisor con RUC 1790012345001 vence el 27 de septiembre de 2026 (hora de Ecuador).",
		"Cuando venza, toda Factura de venta pendiente queda detenida sin firmar — sin secuencial y sin envío — mientras el plazo de 24 horas del SRI para transmitir cada una sigue corriendo.",
		"Suba el .p12 renovado en la página del Emisor:\nhttps://staff.example.test/operator/invoicing/issuer",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want it to contain %q", text, want)
		}
	}
	if strings.Contains(text, "expires") || strings.Contains(text, "Upload") {
		t.Fatalf("text = %q, carries English", text)
	}
}

// TestCertificateExpiryWarningAtExpirySaysItHasLapsed: on the day itself the
// tense changes — the date is in the past, and "expires on" would send the
// reader to check a calendar that says today.
func TestCertificateExpiryWarningAtExpirySaysItHasLapsed(t *testing.T) {
	w := aCertificateExpiryWarning(0, LocaleEN)
	if text := w.Text(); !strings.Contains(text, "The signing certificate of the Issuer with RUC 1790012345001 expired on 27 September 2026 (Ecuador time).") {
		t.Fatalf("text = %q, want the past tense", text)
	}
	if text := w.Text(); strings.Contains(text, "Once it lapses") {
		t.Fatalf("text = %q, still speaks of a lapse to come", text)
	}
	if text := w.Text(); !strings.Contains(text, "Every Sale Invoice owed is now parked unsigned") {
		t.Fatalf("text = %q, want the present consequence", text)
	}

	w.Locale = LocaleES
	if text := w.Text(); !strings.Contains(text, "El certificado de firma del Emisor con RUC 1790012345001 venció el 27 de septiembre de 2026 (hora de Ecuador).") {
		t.Fatalf("text = %q, want the Spanish past tense", text)
	}
	if text := w.Text(); !strings.Contains(text, "Toda Factura de venta pendiente queda ahora detenida sin firmar") {
		t.Fatalf("text = %q, want the Spanish present consequence", text)
	}
}

// TestCertificateExpiryWarningDatesTheCertificateInEcuadorTime pins the zone
// rule on its own: the same instant is the 28th in UTC and the 27th in
// Guayaquil, and both languages must say the 27th.
func TestCertificateExpiryWarningDatesTheCertificateInEcuadorTime(t *testing.T) {
	for _, locale := range []Locale{LocaleEN, LocaleES} {
		text := aCertificateExpiryWarning(1, locale).Text()
		if strings.Contains(text, "28") {
			t.Fatalf("%s text = %q, names the UTC day", locale, text)
		}
		if !strings.Contains(text, "27") {
			t.Fatalf("%s text = %q, want the Ecuadorian day", locale, text)
		}
	}
}

// TestCertificateExpiryWarningCarriesNoCountsAndNoBuyer: the mail says what
// the certificate is about to do and where to fix it, and nothing about the
// documents or the people behind them.
func TestCertificateExpiryWarningCarriesNoCountsAndNoBuyer(t *testing.T) {
	text := aCertificateExpiryWarning(0, LocaleEN).Text()
	for _, forbidden := range []string{"@", "document(s)", "documents are", "invoices are"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("text = %q, contains %q", text, forbidden)
		}
	}
}

// The logging sender prints it and the noop sender accepts it, like every
// other message on the interface.
func TestCertificateExpiryWarningIsAcceptedByTheLoggingAndNoopSenders(t *testing.T) {
	logger := &recordingLogger{}
	if err := (&LoggingEmailSender{Logger: logger}).SendCertificateExpiryWarning(t.Context(), aCertificateExpiryWarning(7, LocaleES)); err != nil {
		t.Fatalf("logging sender: %v", err)
	}
	if logger.infos != 1 || logger.errors != 0 {
		t.Fatalf("logging sender logged infos=%d errors=%d, want the warning announced once", logger.infos, logger.errors)
	}
	if err := (NoopEmailSender{}).SendCertificateExpiryWarning(t.Context(), aCertificateExpiryWarning(7, LocaleES)); err != nil {
		t.Fatalf("noop sender: %v", err)
	}
}
