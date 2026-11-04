package m

import "strconv"

// litres parses s, as a whole number when it is one.
func litres(s string) (float64, error) {
	if n, err := strconv.Atoi(s); err == nil {
		v := float64(n)
		return v, err
	}
	return strconv.ParseFloat(s, 64)
}

func total(s string) (float64, error) {
	v, err := litres(s)
	return v * 2, err
}
