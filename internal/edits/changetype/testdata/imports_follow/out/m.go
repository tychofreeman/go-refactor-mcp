package m

import "io"

type sink = io.StringWriter

func write(w sink, s string) { w.WriteString(s) }
