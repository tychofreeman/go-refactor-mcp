package m

import (
	"encoding/json"
	"fmt"
)

type rec struct {
	Name string
}

func logf(format string, args ...any) {
	fmt.Printf(format, args...)
	b, _ := json.Marshal(args)
	fmt.Println(string(b))
}

func Show(r rec) { logf("%+v", r) }
