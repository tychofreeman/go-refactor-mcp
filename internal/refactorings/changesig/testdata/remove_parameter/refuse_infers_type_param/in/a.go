package m

func first[T any](n int, fallback T) int { return n }

var X = first(1, "a")
