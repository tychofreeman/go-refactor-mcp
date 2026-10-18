package m

type code struct {
	Body []int
	node []int
}

type doc struct{ Body []int }

func (c *code) size() int { return len(c.Body) }

func (c *code) first() int { return c.Body[0] }

func (d *doc) size() int { return len(d.Body) }
