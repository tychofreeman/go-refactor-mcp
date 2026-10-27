package m

func kind(s string) int {
	switch s {
	case "a":
		return 1
	// Anything else is unknown.
	default:
		return 0
	}
}
