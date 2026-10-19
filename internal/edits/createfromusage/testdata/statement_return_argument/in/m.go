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
