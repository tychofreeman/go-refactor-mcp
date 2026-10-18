package m

import "strings"

func loud(t string) string { return strings.ToUpper(t) + "!" }

func Other() string { return loud("b") }
