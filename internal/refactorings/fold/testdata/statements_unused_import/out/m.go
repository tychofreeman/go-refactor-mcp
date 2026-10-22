package m

import (
	"fmt"
)

type W struct{ Sent []string }

func (w *W) Send(to string) {
	record(&w.Sent, "w", to)
}

func (w *W) String() string { return fmt.Sprint(w.Sent) }
