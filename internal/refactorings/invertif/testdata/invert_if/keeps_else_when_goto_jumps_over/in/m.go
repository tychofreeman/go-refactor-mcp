package m

import "fmt"

func run(ok bool) {
	goto done
	if ok {
		n := 1
		fmt.Println(n)
	} else {
		return
	}
done:
	fmt.Println("done")
}
