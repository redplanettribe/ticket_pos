package platform

import "strings"

// NormalizeEmail is the single point at which an email address is normalised:
// trimmed and lowercased.
//
// Every entry path that resolves or creates a Customer by email — Sale Import,
// In-Person Sale, Online Sale, passcode sign-in — goes through this one
// function, so letter case or stray whitespace from two different box offices
// cannot fragment one person's history across several records. The
// platform-global Customer rests on UNIQUE(email) plus exactly one
// normalisation rule; a second implementation of that rule is a way for one
// person to silently become two Customers.
//
// ONE such second implementation exists, deliberately, and must move whenever
// this does: sales.HoldsFilter.BuyerEmailExpr folds a pending Payment's verbatim
// customer_email in SQL, because a Capacity Hold is matched before any Customer
// record exists and Postgres cannot call this function (ADR 0025). That fold is
// `lower(btrim(...))`, which is lower+trim and no more — so if this rule ever
// grows beyond lower+trim, that expression becomes wrong and nothing here will
// fail to tell you. Note the two already differ at the margin: TrimSpace strips
// all Unicode whitespace where btrim's default strips ASCII spaces alone.
//
// The OTP primitive normalises with it too, so rate-limit counters and lookups
// key on the same string the Customer record does. Staff identity uses it for
// the same reason: a Member email and a Staff Session email must compare equal
// regardless of how either was typed.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
