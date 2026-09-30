package booking

import "time"

type Booking struct {
	ID    string
	Room  string
	Start time.Time
	End   time.Time
}
