package m

import (
	"os"
	"strings"
)

func F(path string, xs []string) (string, error) {
	joined := strings.Join(xs, ",")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return joined + string(data), nil
}
