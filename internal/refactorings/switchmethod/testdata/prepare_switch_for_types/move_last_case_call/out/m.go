package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Renewable() bool
}

var kinds = map[string]Kind{"dvd": DVD{}}

// DVD is the Kind of "dvd".
type DVD struct{}

func (DVD) Renewable() bool {
	return true
}

func kind(it Item) string {
	println(it.Title)
	return it.Kind
}

func Renewable(it Item) bool {
	if k, ok := kinds[kind(it)]; ok {
		return k.Renewable()
	}
	switch kind(it) {
	}
	return false
}
