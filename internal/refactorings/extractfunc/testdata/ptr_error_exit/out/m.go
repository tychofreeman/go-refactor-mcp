package m

import (
	"encoding/json"
	"fmt"
)

type config struct{ Name string }

func load(data []byte) (string, error) {
	var cfg config
	p := &cfg
	if err := decode(data, p, &cfg); err != nil {
		return "", err
	}
	return cfg.Name, nil
}

func decode(data []byte, p *config, cfg *config) error {
	if err := json.Unmarshal(data, p); err != nil {
		return fmt.Errorf("decode %v: %w", *cfg, err)
	}
	cfg.Name += "!"
	return nil
}
