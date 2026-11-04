package m

import "fmt"

func Report(plants []string) {
	n := len(plants)
	fmt.Println("plants:", n)
	waterings := 3 * len(plants)
	fmt.Println("waterings:", waterings)
}
