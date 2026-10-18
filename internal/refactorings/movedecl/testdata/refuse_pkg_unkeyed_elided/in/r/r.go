package r

import "example.com/m/p"

type W struct{ t p.T }

func Use() []W { return []W{{t: p.T{}}} }
