package m

func Use() string {
	s := &Stack[string]{}
	s.Push("a")
	s.PushAll([]string{"b", "c"})
	return s.At(0)
}
