package m

import "fmt"

type T struct{}

func (T) String() string { return "T" }

func Use() string { return fmt.Sprint(T{}) + T{}.String() }
