package m

func head(s []int, n int) []int { return s[:n] }

func tail(s []int, n int) []int { return s[n:] }

func Use(s []int) ([]int, []int) { return head(s, 1), tail(s, 1) }
