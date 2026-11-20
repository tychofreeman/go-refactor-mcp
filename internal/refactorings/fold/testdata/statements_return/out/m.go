package m

import "fmt"

type Bed struct{ Heights []int }

func total(b *Bed) int {
	sum := 0
	for _, h := range b.Heights {
		sum += h
	}
	return sum
}

func Tallest(b *Bed) int {
	fmt.Println("bed")
	return total(b)
}

func Report(b *Bed) string {
	sum := total(b)
	return fmt.Sprint(sum)
}
