package m

import "log"

type Wallet struct{ Charged []string }

type Voucher struct{ Charged []string }

func book(charged *[]string, method, to, text string) {
	log.Printf("%s charge to %s", method, to)
	*charged = append(*charged, to+": "+text)
}

func (w *Wallet) Charge(to, ref string) {
	book(&w.Charged, "wallet", to, ref)
}

func (v *Voucher) Charge(to, ref string) {
	book(&v.Charged, "voucher", to, ref)
}

func Local(to string) []string {
	var out []string
	book(&out, "local", to, "x")
	return out
}
