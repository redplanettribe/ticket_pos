package service

// See types_money.go for why catalog's Event type is re-declared here rather
// than referenced through an aliased import.

import "github.com/peter/ticket_pos/backend/internal/catalog/service"

// Event is one of an Organization's Events as the operator sees it (catalog
// owns it).
type Event = service.OperatorEvent
