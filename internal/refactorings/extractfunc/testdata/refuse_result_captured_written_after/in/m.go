package m

type conn struct{ open bool }

func (c *conn) Close() { c.open = false }

func connect() *conn { return &conn{open: true} }

var cleanups []func()

func setup() *conn {
	cs := connect()
	cleanups = append(cleanups, func() { cs.Close() })
	cs = connect()
	return cs
}
