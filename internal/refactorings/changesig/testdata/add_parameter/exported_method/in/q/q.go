package q

import "example.com/m/p"

type Scaler interface{ Scale(by float64) float64 }

func Use(v any) int {
	if _, ok := v.(Scaler); ok {
		return 0
	}
	return p.T{}.Scale(3)
}
