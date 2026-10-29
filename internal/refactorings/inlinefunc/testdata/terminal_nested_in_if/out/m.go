package m

type planner struct {
	placed []string
	count  int
}

func (p *planner) placeIf(force bool, bed string) {
	if force {
		if bed == "" {
			return
		}
		p.placed = append(p.placed, bed)
	}
}
