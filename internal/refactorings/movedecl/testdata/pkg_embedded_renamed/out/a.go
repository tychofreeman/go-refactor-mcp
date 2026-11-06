package m

import "example.com/m/sub"

type inliner struct {
	sub.Place
	n int
}

func make1() inliner { return inliner{Place: sub.Place{Path: "x"}, n: 1} }

func (i inliner) where() string { return i.Path + i.Place.Path }
