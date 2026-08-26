package service

// See types_money.go for why invoicing's list row is re-declared here rather
// than referenced through an aliased import.

import "github.com/peter/ticket_pos/backend/internal/invoicing/service"

// Document is one Tax Invoice as the Sale lookup lists it (#477, #486): the
// invoicing list's own row — kind, state, number, Sale Confirmation
// reference, Recipient, total — so the operator meets one description of a
// document wherever they meet it, with its role in the Sale's chain, the
// links to its neighbours and the reissue's trail beside it; its id is the
// link to the detail.
type Document = service.SaleDocument
