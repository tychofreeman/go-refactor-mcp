package m

type site struct{ Path string }

type inliner struct {
	site
	n int
}

func make1() inliner { return inliner{site: site{Path: "x"}, n: 1} }

func (i inliner) where() string { return i.Path + i.site.Path }
