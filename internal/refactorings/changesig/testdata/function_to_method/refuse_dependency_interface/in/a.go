package m

import "io"

type source struct{ b byte }

func fill(s *source, p []byte) (int, error) { p[0] = s.b; return 1, nil }

func copyTo(w io.Writer, s *source) { var p [1]byte; fill(s, p[:]); w.Write(p[:]) }
