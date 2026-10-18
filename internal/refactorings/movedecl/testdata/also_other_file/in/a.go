package m

import "strings"

func first() string { return strings.ToUpper(second()) }

func Use() string { return first() }
