package m

type Config struct{ port int }

func Port(c Config) *int {
	return &c.port
}
