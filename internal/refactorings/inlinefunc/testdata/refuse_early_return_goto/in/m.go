package m

func Use(n int) int {
	v := pick(n)
	return v
}

func pick(n int) int {
	if n > 0 {
		goto done
	}
	return 0
done:
	return 1
}
