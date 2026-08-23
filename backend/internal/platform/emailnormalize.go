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
// ONE such second implementation exists, deliberately: NormalizeEmailSQL below,
// which folds a stored address in SQL because some comparisons happen where
// Postgres cannot call this function — a Capacity Hold matched before any
// Customer record exists (ADR 0025), a Holder's address compared against the
// buyer's inside a single query (#392). It is a FUNCTION and not a copied
// expression precisely so that "the SQL shadow of NormalizeEmail" has one home
// (#398), and TestNormalizeEmailSQLFoldsTheSameWayAsNormalizeEmail pins the two
// together in a live database.
//
// The OTP primitive normalises with it too, so rate-limit counters and lookups
// key on the same string the Customer record does. Staff identity uses it for
// the same reason: a Member email and a Staff Session email must compare equal
// regardless of how either was typed.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeEmailSQL is the SQL shadow of NormalizeEmail: the expression that
// folds an already-stored address the way NormalizeEmail folds a Go string, for
// the comparisons that happen inside a query.
//
// ONE HOME FOR THE FOLD (#398). It was written out longhand at each site that
// needed it, and two longhand copies of a rule are two rules the day one of them
// is edited — a drift here does not fail a build or a test, it quietly decides
// whether a Holder is recognised as the buyer, and so whether somebody is
// mailed. Callers name a column or a placeholder and get the one expression.
//
// expr IS A COMPILE-TIME SQL EXPRESSION — a column reference or a placeholder
// chosen by the caller, never user input, exactly as sales.HoldsFilter's
// expressions are. The values themselves travel as query arguments.
//
// IT IS lower+trim AND NO MORE, which is what NormalizeEmail is today. If that
// rule ever grows — stripping dots, folding subaddresses — this expression
// becomes wrong, and the pinning test is what will say so rather than a
// production inbox.
//
// THE TWO ALREADY DIFFER AT THE MARGIN, and shipped migrations rest on it:
// strings.TrimSpace strips all Unicode whitespace where btrim's default strips
// ASCII spaces alone. An address with a tab around it therefore folds one way in
// Go and another in Postgres. Nothing writes such an address — every entry path
// normalises in Go before storing — so the divergence is unreachable rather than
// harmless, and narrowing it is not this function's business.
func NormalizeEmailSQL(expr string) string {
	return "lower(btrim(" + expr + "))"
}
