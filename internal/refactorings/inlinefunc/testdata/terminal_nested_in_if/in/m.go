package m

type planner struct {
	placed []string
	count  int
}

func (p *planner) placeIf(force bool, bed string) {
	if force {
		p.place(bed)
	}
}

func (p *planner) place(bed string) {
	if bed == "" {
		return
	}
	p.placed = append(p.placed, bed)
}
