package p

import "strings"

func helper(s string) int { return len(strings.TrimSpace(s)) }

// Two doubles the trimmed length.
func Two(s string) int { return helper(s) * 2 }

func Keep() int { return 0 }
