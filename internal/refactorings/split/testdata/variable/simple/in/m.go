package m

import "fmt"

func Report(plants []string) {
	n := len(plants)
	fmt.Println("plants:", n)
	n = 3 * len(plants)
	fmt.Println("waterings:", n)
}
