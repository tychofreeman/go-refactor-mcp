package m

import "fmt"

// feed feeds bed, twice if hungry.
func feed(bed string, hungry bool) {
	if hungry {
		n := 2
		fmt.Println("feed", bed, n)
	}
	n := 1
	fmt.Println("feed", bed, n)
}

func feedHungry(b string) { feed(b, true) }
