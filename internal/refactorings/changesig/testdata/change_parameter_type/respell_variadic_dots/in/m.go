package m

type name = string

func join(sep string, parts ...string) string { return sep + parts[0] }

func use() string { return join(",", "a") }
