package service

// See types_money.go for why identity's Organization type is re-declared here
// rather than referenced through an aliased import.

import "github.com/peter/ticket_pos/backend/internal/identity/service"

// Organization is one Organization as the operator sees it (identity owns it).
type Organization = service.OperatorOrganization
