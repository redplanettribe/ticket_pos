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

// PayoutRequestSummary is one ask as a LIST shows it: the account number already
// masked and the Tax ID absent (sales owns it, and does the masking).
type PayoutRequestSummary = service.OperatorPayoutRequest

// PayoutRequestWhole is one ask entire, snapshot bank details included — the
// detail view an operator opens when they are about to make the transfer, as
// against the masked PayoutRequestSummary a list shows.
//
// It is NOT named PayoutRequest, and must not be renamed to it. Both this
// package and the sales package it aliases are called `service`, so an alias
// whose own name matched its target's would read as `service.PayoutRequest =
// service.PayoutRequest` — which the swagger generator resolves to itself and
// gives up on, failing `make openapi` with a recursion error rather than
// anything that names the real problem. Every other alias here is safe only
// because sales happens to have prefixed its own type differently.
type PayoutRequestWhole = service.PayoutRequest

// PayoutFulfilment is what a fulfilled Payout Request produces: the Payout now
// in the ledger, and the request it answered (sales owns both).
type PayoutFulfilment = service.FulfilledPayoutRequest

// SaleReversal is one recorded Operator Reversal — the marked sale and the
// money memo the operator asserted (sales owns it).
type SaleReversal = service.OperatorReversalResult
