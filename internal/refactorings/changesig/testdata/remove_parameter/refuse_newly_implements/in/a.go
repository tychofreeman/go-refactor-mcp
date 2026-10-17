package m

import "io"

type F struct{}

func (F) Close(force bool) error { return nil }

var _ = io.EOF

func Use(f F) error { return f.Close(true) }
