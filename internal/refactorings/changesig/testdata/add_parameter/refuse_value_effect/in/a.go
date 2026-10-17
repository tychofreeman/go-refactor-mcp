package m

var seq int

func show(s string) string { return s }

func next() int { seq++; return seq }

func Use() string { return show("a") }
