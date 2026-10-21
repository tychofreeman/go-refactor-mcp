package m

import "bytes"

type sink = *bytes.Buffer

func write(w sink, s string) { w.WriteString(s) }
