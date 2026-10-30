package m

type typer interface{ Type() any }

type target struct{ calls []int }

func (t *target) find(n int) string {
	t.calls = append(t.calls, n)
	return ""
}

func forFn(t *target, fn typer, n int, f func(n int) string) string {
	if fn.Type() == nil {
		return "no type"
	}
	count := func() int { return len(t.calls) }
	find := f
	return find(n) + string(rune(count()))
}

func use(t *target, fn typer) string {
	return forFn(t, fn, 1, t.find)
}
