package m

type soil struct {
	ph    int
	depth int
}

type bed struct {
	soil soil
	name string
}

func newBed() bed { return bed{name: "a", soil: soil{ph: 7, depth: 3}} }

func other() bed { return bed{soil: soil{depth: 2}} }

func (b bed) deep() bool { return b.soil.depth > 2 }

func (b *bed) dig() { b.soil.depth++ }

type plot struct{ bed }

func (p plot) deeper() int { return p.soil.depth + 1 }
