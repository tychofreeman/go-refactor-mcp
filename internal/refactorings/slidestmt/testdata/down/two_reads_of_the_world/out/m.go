package m

import (
	"os"
	"strings"
)

func F(path string, xs []string) (string, error) {
	data, err := os.ReadFile(path)
	joined := strings.Join(xs, ",")
	if err != nil {
		return "", err
	}
	return joined + string(data), nil
}
