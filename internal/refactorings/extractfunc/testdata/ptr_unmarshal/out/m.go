package m

import (
	"encoding/json"
	"fmt"
)

type config struct{ Name string }

func load(data []byte) {
	var cfg config
	decode(data, &cfg)
	fmt.Println(cfg.Name)
}

func decode(data []byte, cfg *config) {
	fmt.Println("loading")
	_ = json.Unmarshal(data, cfg)
}
