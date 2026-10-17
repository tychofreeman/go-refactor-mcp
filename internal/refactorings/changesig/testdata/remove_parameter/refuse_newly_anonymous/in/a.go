package m

type T struct{}

func (T) Size(n int) int { return 1 }

func Has(v any) bool {
	_, ok := v.(interface{ Size() int })
	return ok
}
