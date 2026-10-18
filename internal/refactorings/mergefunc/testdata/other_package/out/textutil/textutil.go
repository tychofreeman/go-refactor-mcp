package textutil

// Plural returns "s" unless count is 1.
func Plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
