package m

import "example.com/dep/strutil"

// Name is a name.
const Name = "m"

// Title is Name, upper-cased.
func Title() string { return strutil.Up(Name) }
