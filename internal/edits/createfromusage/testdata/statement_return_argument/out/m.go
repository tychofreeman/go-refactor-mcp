package m

import "strings"

type Config struct{ Name string }

func Run(cfg Config, xs []int) (bool, error) {
	Apply(cfg, xs, cfg.Name, len(xs))
	if strings.ToUpper(Title(cfg)) == "" {
		return false, nil
	}
	return check(xs)
}

func Apply(cfg Config, xs []int, name string, n int) {
	panic("not implemented")
}

func Title(cfg Config) string {
	panic("not implemented")
}

func check(xs []int) (bool, error) {
	panic("not implemented")
}
