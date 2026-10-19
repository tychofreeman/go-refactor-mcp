package m

import "strconv"

// parse never reads the err it is given: the first statement replaces it.
func parse(s string, err error) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func Use() (int, error) { return parse("1", nil) }
