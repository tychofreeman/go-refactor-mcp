package m

import "log"

type Card struct{ Charged []string }

func last(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func book(charged *[]string, method, to, text string) {
	log.Printf("%s charge to %s", method, to)
	*charged = append(*charged, to+": "+text)
}

func (c *Card) Charge(number, ref string) {
	book(&c.Charged, "card", last(number, 4), ref)
}
