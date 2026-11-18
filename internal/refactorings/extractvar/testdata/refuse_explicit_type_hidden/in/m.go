package m

type Flag bool

func set(f Flag) Flag { return f }

func Use(a, b int) Flag {
	type Flag int
	_ = Flag(0)
	return set(a == b)
}
