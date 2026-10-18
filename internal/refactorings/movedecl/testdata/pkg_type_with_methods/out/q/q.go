package q

import "fmt"

func Base() int { return 1 }

// Counter counts.
type Counter struct{ n int }

// Inc adds one.
func (c *Counter) Inc() { c.n++ }

func (c Counter) String() string { return fmt.Sprint(c.n) }
