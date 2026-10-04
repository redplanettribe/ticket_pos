package repository

import (
	"slices"
	"testing"
)

// TestNamedTicketsOfPairsEachAddressWithItsTicket pins the pairing the commit
// makes between the addresses a Payment holds and the Tickets it minted: index
// n of a Ticket Type is ordinal n of that Ticket Type's line, an address naming
// no minted Ticket is skipped rather than failing a paid commit, and the
// buyer's seat is never handed to anybody, whatever is held for it.
//
// A unit test because two of the three cases are unreachable over HTTP:
// begin-checkout holds no address for the seat and none past a line's quantity.
func TestNamedTicketsOfPairsEachAddressWithItsTicket(t *testing.T) {
	minted := map[string]map[int]string{
		"ga":  {1: "ga-1", 2: "ga-2", 3: "ga-3"},
		"vip": {1: "vip-1", 2: "vip-2"},
	}
	held := []HeldHolder{
		{TicketTypeID: "ga", TicketIndex: 3, HolderEmail: "cai@example.com"},
		{TicketTypeID: "vip", TicketIndex: 1, HolderEmail: "seat@example.com"},
		{TicketTypeID: "vip", TicketIndex: 2, HolderEmail: "ben@example.com"},
		{TicketTypeID: "ga", TicketIndex: 4, HolderEmail: "past-the-line@example.com"},
		{TicketTypeID: "free", TicketIndex: 1, HolderEmail: "surrendered@example.com"},
		{TicketTypeID: "ga", TicketIndex: 1, HolderEmail: "cai@example.com"},
	}

	got := namedTicketsOf(held, minted, "vip-1")

	want := []namedTicket{
		{ticketID: "ga-3", holderEmail: "cai@example.com"},
		{ticketID: "vip-2", holderEmail: "ben@example.com"},
		{ticketID: "ga-1", holderEmail: "cai@example.com"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("namedTicketsOf = %+v, want %+v", got, want)
	}
}

// TestNamedTicketsOfNamesNothingWithoutHolders: every route but a Named Tickets
// checkout leaves the term empty, and an empty term names nobody.
func TestNamedTicketsOfNamesNothingWithoutHolders(t *testing.T) {
	if got := namedTicketsOf(nil, map[string]map[int]string{"ga": {1: "ga-1"}}, ""); len(got) != 0 {
		t.Fatalf("namedTicketsOf(nil) = %+v, want nothing", got)
	}
}
