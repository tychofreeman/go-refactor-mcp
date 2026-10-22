package m

import "fmt"

type Bank struct{ Charged []string }

type Card struct{ Charged []string }

type Voucher struct{ Charged []string }

var count int

func next() string {
	count++
	return fmt.Sprint(count)
}

func book(charged *[]string, to, text string) {
	*charged = append(*charged, to+": "+text)
}

func (b *Bank) Charge(to, ref string, n int) {
	book(&b.Charged, to, ref+fmt.Sprintf(" %d", n))
}

func (c *Card) Charge(first, last, ref string) {
	book(&c.Charged, first+" "+last, ref)
}

func (v *Voucher) Charge(to, ref string) {
	v.Charged = append(v.Charged, to+": "+ref+next())
}
