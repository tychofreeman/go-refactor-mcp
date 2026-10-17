package m

import "fmt"

func run() {
	k := "outer"
	{
		const k = 3
		fmt.Println(k)
	}
	fmt.Println(k)
}
