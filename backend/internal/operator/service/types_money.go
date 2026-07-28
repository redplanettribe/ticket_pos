package service

// The money types the operator payloads carry, re-declared here as aliases of
// the sales types that define them. These are aliases, not copies: there is
// still exactly one definition of a Payout, a Pagination block and a currency's
// totals, and sales still owns it.
//
// The indirection exists for the OpenAPI generator: swag (v2.0.0-rc5) resolves a
// struct field's type through the imports of the file that declares the field,
// and it cannot resolve an *aliased* import. Every module's service package is
// named `service`, so a file that references two of them must alias at least
// one — which swag would then fail to parse. Declaring each foreign type in its
// own file, under an import that needs no alias, keeps every payload struct
// referring to local names that swag can follow.

import "github.com/peter/ticket_pos/backend/internal/sales/service"

// The two names below deliberately differ from the sales names they alias:
// this package is itself called `service`, so an alias whose name matched its
// target would read as `service.X = service.X` and swag would parse it as a
// self-recursive definition.

// PlatformTotals is the platform's own money in one currency (sales owns it).
type PlatformTotals = service.CurrencyTotals

// PageInfo is the ADR-0006 pagination block.
type PageInfo = service.Pagination

// Payout is one recorded Payout with its audit trail.
type Payout = service.OperatorPayout

// Sale is one Ticket Sale as the operator's lookup by Sale Confirmation
// reference returns it (sales owns it).
type Sale = service.OperatorSale
