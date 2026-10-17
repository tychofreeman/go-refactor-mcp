package q

func Quad(n int) int { return Double(Double(n)) }

func Double(n int) int { return 2 * n }
