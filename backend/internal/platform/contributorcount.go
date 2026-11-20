package platform

// EventCount is how many of something the platform counted for one Event
// over a window the caller of the read named. It carries the Event's name
// and its Organization's id and name, which is what an operator page needs
// to state the Event and link on from it. It is what each module hands
// Platform Trends for the breakdown of a figure that is one plain count.
type EventCount struct {
	EventID          string
	EventName        string
	OrganizationID   string
	OrganizationName string
	Count            int
}

// OrganizationCount is how many of something the platform counted for one
// Organization over a window the caller of the read named, with the
// Organization's name.
type OrganizationCount struct {
	OrganizationID   string
	OrganizationName string
	Count            int
}
