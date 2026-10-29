package m

import "log"

type rec struct {
	Name  string
	total int
}

func Show(r rec) { log.Printf("%+v", r) }
