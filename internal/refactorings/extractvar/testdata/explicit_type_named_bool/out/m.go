package m

type Flag bool

func Over(x, limit int) Flag {
	var over Flag = x > limit
	var f Flag = over
	return f
}
