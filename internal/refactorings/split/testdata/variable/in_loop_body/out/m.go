package m

import "fmt"

func Each(beds []int) {
	for _, b := range beds {
		w := b * 2
		fmt.Println(w)
		late := b + 1
		fmt.Println(late)
	}
}
