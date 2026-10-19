package m

type Name string

func Pair(a string, b Name) string { return a + string(b) }

func Run(y string) string { return Pair("x", Name(y)) }
