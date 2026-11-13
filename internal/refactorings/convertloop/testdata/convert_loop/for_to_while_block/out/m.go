package m

import "fmt"

func count() {
	{
		i := 0
		for i < 3 {
			fmt.Println(i)
			i++
		}
	}
	for i := 0; i < 2; i++ {
		fmt.Println(-i)
	}
}
