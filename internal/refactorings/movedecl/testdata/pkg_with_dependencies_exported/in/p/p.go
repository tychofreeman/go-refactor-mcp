package p

// helper gives one.
func helper() int { return 1 }

// Two is two.
func Two() int { return helper() + 1 }

// Other stays, and still needs helper.
func Other() int { return helper() }
