package m

var (
	Host = "localhost"
	Port = 8080
)

func Addr() (string, int) { return Host, Port }
