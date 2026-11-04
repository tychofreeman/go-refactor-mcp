package m

import "strconv"

func total(s string) (float64, error) {
	var v float64
	var result float64
	var err error
	var result2 error
	if n, err := strconv.Atoi(s); err == nil {
		v := float64(n)
		result, result2 = v, err
	} else {
		result, result2 = strconv.ParseFloat(s, 64)
	}
	v = result
	err = result2
	return v * 2, err
}
