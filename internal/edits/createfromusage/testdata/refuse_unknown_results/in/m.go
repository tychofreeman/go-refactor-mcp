package m

type Config struct{}

func Run() {
	c := Load("x")
	_ = c
}
