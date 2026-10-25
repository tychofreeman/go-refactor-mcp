package m

import "encoding/json"

var valid = json.Valid([]byte(`{}`))

func Keep() int { return 1 }
