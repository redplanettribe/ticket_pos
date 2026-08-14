package platform

// ResolveStaffLocale answers, for a piece of staff mail, the question
// ResolveMailLocale answers for a Customer's: what language is this written in?
//
// It is a one-candidate chain and it stays a function for the same reason the
// Customer one is: the ENGLISH FLOOR is a decision, and a decision spelled out
// at four send sites is a decision that will be spelled differently at the
// fifth. Staff has one candidate where Customer mail has two, because a person's
// Staff Locale reaches the screen and the inbox alike — the second term exists
// on the Storefront only because a page property cannot reach an email
// (ADR 0041).
//
// stored is the value as the row holds it, and "" is the ordinary answer: nobody
// is given a language by being invited, imported or paid out, so a person who
// has never signed in or never chosen has stated nothing. That is ABSENCE and
// not English — the distinction is what lets the next sign-in record a detected
// language — and English is what this function puts underneath it, for the
// reader, without the storage ever having to claim it.
//
// It is read through ParseLocale, so a value this platform writes nothing in is
// skipped rather than written in. Nothing here can fail: a language is not worth
// refusing a passcode or a notice about somebody's money over.
func ResolveStaffLocale(stored string) Locale {
	if locale, ok := ParseLocale(stored); ok {
		return locale
	}
	return DefaultLocale
}
