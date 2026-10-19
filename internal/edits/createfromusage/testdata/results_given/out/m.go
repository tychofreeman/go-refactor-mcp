package m

type Config struct{}

func Run() {
	c := Load("x")
	_ = c
}

func Load(s string) *Config {
	panic("not implemented")
}
