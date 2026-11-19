package a

// S is a shipment.
type S struct {
	Road   string
	Routes map[string]string
}

// All are the shipments.
var All = []S{
	{Road: "nw"},
	{Road: "nw"},
}
