package m

type Plant interface {
	Height() int
}

type Fern struct{}

func (Fern) Height() int { return 1 }

type Oak struct{}

func (Oak) Height() int { return 9 }

func Water(p Plant, bed string) string {
	switch p.(type) {
	case Fern:
		return bed + ": mist"
	case Oak:
		return bed + ": soak"
	}
	return bed
} // Water ends here
