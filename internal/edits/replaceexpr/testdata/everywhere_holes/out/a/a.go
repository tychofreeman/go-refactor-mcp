package a

// S is a shipment.
type S struct {
	Road   string
	Routes map[string]string
}

// T has a field of the same name.
type T struct{ Road string }

// F reads s's road.
func F(s S) string { return s.Routes["road"] }

// G reads t's road.
func G(t T) string { return t.Road }
