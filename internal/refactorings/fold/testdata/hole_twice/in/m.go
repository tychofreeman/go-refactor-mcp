package m

var k int

func next() int { k++; return k }

func double(n int) int { return n + n }

func f(x, y int) int { return x + x + (next() + next()) + (x + y) }
