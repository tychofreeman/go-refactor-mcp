package m

type myErr struct{}

func (*myErr) Error() string { return "bad" }

func run(n int) error {
	var err error
	err = check(n)
	return err
}

func check(n int) *myErr {
	if n < 0 {
		return &myErr{}
	}
	return nil
}
