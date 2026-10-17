package m

type shower interface{ show() }

func run(s shower) {
	s.show()
}
