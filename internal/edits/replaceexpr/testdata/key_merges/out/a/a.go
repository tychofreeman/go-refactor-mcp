package a

// S is a shipment.
type S struct {
	ID     string
	Rail   string
	Routes map[string]string
}

// All are the shipments.
var All = []S{
	{Routes: map[string]string{"rail": "Y9"}},
	{ID: "2", Routes: map[string]string{"road": "nw-0042", "rail": "Y7"}},
	{Routes: map[string]string{"road": "se-1111", "rail": "Y8"}},
	{Routes: map[string]string{"rail": "Y6"}},
	{
		ID:     "3",
		Routes: map[string]string{"road": "sw-2222", "rail": "Y5"},
	},
}
