package m

type userID = string

func greet(id userID, n int) string { return id }

func use(name string) string { return greet(name, 1) }
