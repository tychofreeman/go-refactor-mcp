package m

import "fmt"

func Show(beds []string) {
	for _, b := range beds {
		fmt.Println("bed", b)
		fmt.Println("done", b)
	}
}
