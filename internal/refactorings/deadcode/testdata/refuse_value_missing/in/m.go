package m

type Workspace struct{ Root string }

type Site struct{ WS *Workspace }

type mover struct {
	Site
	ws   *Workspace
	name string
}

func newMover(s Site) *mover {
	return &mover{Site: s, name: "m"}
}

func (m *mover) root() string { return m.ws.Root }

func other(s Site) mover {
	return mover{
		Site: s,
		ws:   s.WS,
	}
}
