package q

import "example.com/m/p"

func Zero() int { return 0 }

// Two is one step past Base.
func Two() int { return p.Base() + p.Step }
