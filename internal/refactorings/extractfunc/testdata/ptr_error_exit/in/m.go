package m

import (
	"encoding/json"
	"fmt"
)

type config struct{ Name string }

func load(data []byte) (string, error) {
	var cfg config
	p := &cfg
	if err := json.Unmarshal(data, p); err != nil {
		return "", fmt.Errorf("decode %v: %w", cfg, err)
	}
	cfg.Name += "!"
	return cfg.Name, nil
}
