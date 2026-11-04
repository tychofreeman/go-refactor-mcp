package m

import "fmt"

type dry struct{}

func (*dry) Error() string { return "dry" }

func find(ok bool) *dry { return nil }

func Check() {
	var err error = fmt.Errorf("wilted")
	fmt.Println(err)
	err = find(true)
	fmt.Println(err != nil)
}
