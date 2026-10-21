package m

import "sort"

type user struct {
	name  string
	email string
}

func byName(us []user) {
	sort.SliceStable(us, func(i, j int) bool { return us[i].name < us[j].name })
}
