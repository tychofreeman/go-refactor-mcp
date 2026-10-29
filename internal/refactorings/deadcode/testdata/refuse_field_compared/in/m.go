package m

import "reflect"

type user struct {
	name  string
	email string
}

func same(a, b user) bool { return reflect.DeepEqual(a, b) }
