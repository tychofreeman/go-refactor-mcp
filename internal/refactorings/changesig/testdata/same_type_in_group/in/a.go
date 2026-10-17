package m

func span(lo, hi int, label string) int { return hi - lo }

func Use() int { return span(1, 9, "a") }
