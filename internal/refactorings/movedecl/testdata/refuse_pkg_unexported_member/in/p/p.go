package p

type T struct{ n int }

func (t *T) bump() { t.n++ }

func (t *T) Get() int { return t.n }
