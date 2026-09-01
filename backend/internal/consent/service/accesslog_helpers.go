package service

import "database/sql"

// The access log's null helpers (#569), in a file of their own so they sit
// beside nothing they could be confused with: this package already has a
// nullBool that goes the other way, into the driver, for capturing consent.

// accessText renders an optional column on the way IN: EMPTY IS ABSENT, so a
// field a caller did not fill travels as NULL rather than as an empty string.
// Migration 116's act-whole CHECK distinguishes the two, and an empty string
// would be a value that satisfied "is not null" while saying nothing.
func accessText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// accessTextOf is the same distinction on the way back OUT.
func accessTextOf(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

func accessBoolOf(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	flag := value.Bool
	return &flag
}

// accessCountOf narrows the driver's int64 to the int the column holds. The
// column is an INTEGER with a `>= 0` CHECK, so nothing here can overflow.
func accessCountOf(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	count := int(value.Int64)
	return &count
}
