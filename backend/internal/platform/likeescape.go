package platform

import "strings"

// LikeEscape escapes the LIKE/ILIKE metacharacters (\, %, _) in a search term so
// it is matched as a LITERAL SUBSTRING and never as a pattern. The backslash is
// Postgres's default ILIKE escape character.
//
// The property it exists for: without it, `%` means "match everything" and `_`
// "match any one character", so a reader who typed either would be handed the
// WHOLE LIST under a filter bar claiming to be narrowed — a screen that says it
// is showing a search result while showing everything.
//
// IT LIVES HERE, in the package that already holds the domain-free primitives
// (OptionalString and its neighbours), because it is a piece of Postgres
// trivia and no domain's property: the Sales list (sales/repository.ListSales)
// and the Tax Invoices list (invoicing/repository.ListInvoices) must not be
// able to disagree about what "search" means, and the two modules cannot
// import each other. The catalog module keeps its own two copies with their
// own stated reasons (holderRosterLikeEscape, escapeLike); converting them is
// a cleanup, not this change.
func LikeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}
