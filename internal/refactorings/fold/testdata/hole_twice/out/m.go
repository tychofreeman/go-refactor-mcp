package m

var k int

func next() int { k++; return k }

func double(n int) int { return n + n }

func f(x, y int) int { return double(x) + (next() + next()) + (x + y) }
