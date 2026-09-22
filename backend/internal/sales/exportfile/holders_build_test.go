package exportfile

import (
	"bytes"
	"time"
)

// HolderRoster is a whole Holder Export held at once - the Event's columns and
// every row - for the tests that assert what a file of a few Tickets looks like.
// Production never holds one: it streams rows through HolderExport as they come
// off the database (ADR 0075).
type HolderRoster struct {
	Questions  []QuestionColumn
	Assignment bool
	// Tickets are written in the order given; the file re-sorts nothing.
	Tickets []HolderRow
}

// BuildHolderExport streams a roster through the production writer into
// memory, so these tests read exactly the bytes a download carries.
func BuildHolderExport(roster HolderRoster, loc *time.Location, info HolderInfo) ([]byte, error) {
	var buf bytes.Buffer
	export, err := BeginHolderExport(&buf, HolderColumns{Questions: roster.Questions, Assignment: roster.Assignment}, loc, info.GeneratedAt)
	if err != nil {
		return nil, err
	}
	for _, ticket := range roster.Tickets {
		if err := export.Append(ticket); err != nil {
			return nil, err
		}
	}
	if err := export.Finish(info); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
