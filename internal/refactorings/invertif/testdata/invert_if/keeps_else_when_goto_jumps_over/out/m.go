package m

import "fmt"

func run(ok bool) {
	goto done
	if !ok {
		return
	} else {
		n := 1
		fmt.Println(n)
	}
done:
	fmt.Println("done")
}
