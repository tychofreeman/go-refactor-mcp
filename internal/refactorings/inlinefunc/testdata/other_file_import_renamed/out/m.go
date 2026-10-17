package m

import str "strings"

func run(s string) string {
	_ = str.ToLower
	return str.ToUpper(s)
}
