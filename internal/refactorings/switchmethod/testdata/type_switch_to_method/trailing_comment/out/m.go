package m

type Plant interface {
	Height() int
	water(bed string) string
}

type Fern struct{}

func (Fern) Height() int { return 1 }

type Oak struct{}

func (Oak) Height() int { return 9 }

func Water(p Plant, bed string) string {
	if p != nil {
		return p.water(bed)
	}
	return bed
} // Water ends here

func (Fern) water(bed string) string {
	return bed + ": mist"
}

func (Oak) water(bed string) string {
	return bed + ": soak"
}
