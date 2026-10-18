package m

func one() int { return 1 }

func uno() int { return 1 }

func Use() int {
	one := 2
	return uno() + one
}
