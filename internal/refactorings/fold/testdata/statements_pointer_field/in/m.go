package m

import "log"

type Wallet struct{ Charged []string }

type Voucher struct{ Charged []string }

func book(charged *[]string, method, to, text string) {
	log.Printf("%s charge to %s", method, to)
	*charged = append(*charged, to+": "+text)
}

func (w *Wallet) Charge(to, ref string) {
	log.Printf("%s charge to %s", "wallet", to)
	w.Charged = append(w.Charged, to+": "+ref)
}

func (v *Voucher) Charge(to, ref string) {
	log.Printf("%s charge to %s", "voucher", to)
	v.Charged = append(v.Charged, to+": "+ref)
}

func Local(to string) []string {
	var out []string
	log.Printf("%s charge to %s", "local", to)
	out = append(out, to+": "+"x")
	return out
}
