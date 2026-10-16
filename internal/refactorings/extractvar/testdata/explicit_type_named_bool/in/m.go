package m

type Flag bool

func Over(x, limit int) Flag {
	var f Flag = x > limit
	return f
}
