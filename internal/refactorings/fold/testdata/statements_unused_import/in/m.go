package m

import (
	"fmt"
	"log"
)

type W struct{ Sent []string }

func (w *W) Send(to string) {
	log.Printf("%s to %s", "w", to)
	w.Sent = append(w.Sent, to)
}

func (w *W) String() string { return fmt.Sprint(w.Sent) }
