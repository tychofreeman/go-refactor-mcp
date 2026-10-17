package m

type base struct{}

func (base) hello() {}

type outer struct{ base }

func run(o outer) {
	o.hello()
}
