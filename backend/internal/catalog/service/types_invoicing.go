package service

// The invoicing type the Customer Dossier's port returns (#639), declared under
// an unaliased import in its own file: swag cannot follow an aliased import from
// a file that declares a payload struct (see operator/service/types_money.go).

import "github.com/peter/ticket_pos/backend/internal/invoicing/service"

// InvoicingSaleDocument is one Tax Invoice about a Ticket Sale as invoicing
// reads it for the operator Sale lookup (invoicing owns it).
type InvoicingSaleDocument = service.SaleDocument
