package m

type soil struct{ ph int }

type bed struct {
	soil  soil
	name  string
	depth int
}

func newBed() bed { return bed{name: "a", depth: 3, soil: soil{ph: 7}} }

func other() bed { return bed{depth: 2} }

func (b bed) deep() bool { return b.depth > 2 }

func (b *bed) dig() { b.depth++ }

type plot struct{ bed }

func (p plot) deeper() int { return p.depth + 1 }

var acid = soil{5}
