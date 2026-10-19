package p

const scale = 2

func helper() int { return scale }

// Two is two.
func Two() int { return helper() }

// Other stays.
func Other() int { return 7 }
