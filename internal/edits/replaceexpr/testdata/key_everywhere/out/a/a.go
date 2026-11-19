package a

// S is a shipment.
type S struct {
	ID     string
	Road   string
	Rail   string
	Routes map[string]string
}

// T has a field of the same name.
type T struct{ Road string }

// Ship is a shipment by road.
func Ship(code string) S {
	return S{ID: "1", Routes: map[string]string{"road": code}}
}

// Truck is a T.
func Truck() T { return T{Road: "t"} }
