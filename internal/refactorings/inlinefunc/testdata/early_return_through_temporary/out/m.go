package m

type myErr struct{}

func (*myErr) Error() string { return "bad" }

func run(n int) error {
	var err error
	var result *myErr
	if n < 0 {
		result = &myErr{}
	} else {
		result = nil
	}
	err = result
	return err
}
