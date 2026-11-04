package m

import (
	"fmt"
	"time"
)

// Scheduler has the method Every of Sprinkler.
type Scheduler interface {
	Every(d time.Duration, beds ...string) (n int, err error)
}

var _ Scheduler = (*Sprinkler)(nil)

// Sprinkler waters a bed.
type Sprinkler struct{ rate int }

// Water gives bed litres of water.
func (s *Sprinkler) Water(bed string, litres int) error {
	fmt.Println(bed, litres*s.rate)
	return nil
}

// Rate is how fast it waters.
func (s Sprinkler) Rate() int { return s.rate }

// Every waters each of beds every d.
func (s *Sprinkler) Every(d time.Duration, beds ...string) (n int, err error) { return len(beds), nil }

// Off stops it.
func (s *Sprinkler) Off() {}
