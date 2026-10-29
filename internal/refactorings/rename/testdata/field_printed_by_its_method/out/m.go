package m

import "fmt"

type rec struct {
	Title string
}

func (r rec) String() string { return "rec" }

type pair struct{ a, b rec }

func (p *pair) GoString() string { return "pair" }

func Show(r rec, p *pair) string { return fmt.Sprintf("%+v %#v", r, p) }
