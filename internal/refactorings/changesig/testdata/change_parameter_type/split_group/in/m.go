package m

type Name string

func Pair(a, b string) string { return a + b }

func Run(y string) string { return Pair("x", y) }
