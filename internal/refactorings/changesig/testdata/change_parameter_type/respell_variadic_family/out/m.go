package m

type renderArg = string

type Renderer interface {
	Render(ctx int, layout ...renderArg) string
}

type page struct{}

func (p page) Render(ctx int, layout ...renderArg) string {
	if len(layout) == 0 {
		return ""
	}
	return layout[0]
}

func use(r Renderer, names []string) string {
	return r.Render(1, "a", "b") + r.Render(2) + r.Render(3, names...)
}
