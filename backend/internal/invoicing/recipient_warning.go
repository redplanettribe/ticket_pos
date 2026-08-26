package invoicing

// The Recipient Warning (#482, ADR 0061): the Tax Authority's word, on a Sale
// Invoice it nevertheless AUTHORIZED, that the Recipient's Tax ID does not
// exist or is incorrect. Not a fault of the document, which stands — the
// status is `authorized`, the Drainer settled it, it is delivered and it is
// credited as any other — but the one signal the platform has that the
// factura was declared to the wrong taxpayer, and so the operator's cue for
// a Sale Invoice Reissue. Shown wherever the document is listed until the
// document is superseded; never cleared by delivery, by time or by hand.
//
// A fact of a Sale Invoice alone: a manual Tax Invoice's Recipient was
// typed by the operator, who issues another by hand, and a Credit Note
// names the same Recipient as the factura it credits, so a warning on it
// would say nothing the factura's does not.

// The SRI's advertencias about the Recipient's identification: 59
// "identificación no existe" and 62 "identificación incorrecta". Its 60 —
// "este proceso fue realizado en el ambiente de pruebas" — is on every
// test-environment authorization and says nothing about the Recipient.
const (
	AuthorityMessageIdentificationMissing   = "59"
	AuthorityMessageIdentificationIncorrect = "62"
	// AuthorityMessageTypeWarning is the SRI's tipo for an advertencia.
	AuthorityMessageTypeWarning = "ADVERTENCIA"
)

// RecipientWarningIn reports whether an authorization's messages carry the
// SRI's advertencia 59 or 62: what raises a Recipient Warning on the Sale
// Invoice the messages answer for. Exact identifiers, warning-typed; 60
// alone never does.
func RecipientWarningIn(messages []AuthorityMessage) bool {
	for _, m := range messages {
		if m.Type != AuthorityMessageTypeWarning {
			continue
		}
		switch m.Identifier {
		case AuthorityMessageIdentificationMissing, AuthorityMessageIdentificationIncorrect:
			return true
		}
	}
	return false
}
