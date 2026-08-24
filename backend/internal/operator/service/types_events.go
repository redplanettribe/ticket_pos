package service

// See types_money.go for why catalog's Event type is re-declared here rather
// than referenced through an aliased import.

import "github.com/peter/ticket_pos/backend/internal/catalog/service"

// Event is one of an Organization's Events as the operator sees it (catalog
// owns it).
type Event = service.OperatorEvent

// TicketQuestion is one question on the Operator's Event view: the staff
// payload plus the Ticket Type it hangs off (#410, ADR 0056).
type TicketQuestion = service.OperatorTicketQuestion

// RevokedTicketQuestion is the question as it stands after a Revocation:
// retired, still approved, carrying the reason.
type RevokedTicketQuestion = service.TicketQuestionView
