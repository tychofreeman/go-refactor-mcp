package m

type Soil struct{}

func (Soil) Name() string { return "soil" }

type Bed struct{ Soil }

func (Bed) Name() string { return "bed" }

func Label(b Bed) string { return b.Name() }
