package m

import "log"

type rec struct {
	Name  string
	count int
}

func Show(r rec) { log.Printf("%+v", r) }
