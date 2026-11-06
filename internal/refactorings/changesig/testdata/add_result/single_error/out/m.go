package m

func check(s string) (int, error) {
	return 0, nil
}

func use() error {
	_, err := check("a")
	return err
}
