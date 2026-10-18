package m

type sizer interface{ size() int }

type box struct{}

func (box) size() int { return 1 }

func Has(v any) bool { _, ok := v.(sizer); return ok }
