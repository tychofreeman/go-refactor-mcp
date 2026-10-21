package m

// F is used.
func F() int { return 1 }

// even reports whether n is even, with odd.
func even(n int) bool {
	if n == 0 {
		return true
	}
	return odd(n - 1)
}

// odd reports whether n is odd, with even.
func odd(n int) bool {
	if n == 0 {
		return false
	}
	return even(n - 1)
}

var x = odd(3)
