package m

type Soil struct{}

func (Soil) Name(prefix string) string { return prefix + "soil" }

type Bed struct{ Soil }

func Label(b Bed) string { return b.Name("a ") }
