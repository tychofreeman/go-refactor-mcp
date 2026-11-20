package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Renewable() bool
}

var kinds = map[string]Kind{}

func kind(it Item) string {
	println(it.Title)
	return it.Kind
}

func Renewable(it Item) bool {
	if k, ok := kinds[kind(it)]; ok {
		return k.Renewable()
	}
	switch kind(it) {
	case "dvd":
		return true
	}
	return false
}
