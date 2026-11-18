package p

// Base is where counting starts.
func Base() int { return 1 }

// Step is how far a count goes.
const Step = 1

// Two is one step past Base.
func Two() int { return Base() + Step }
