package a

// S is a shipment.
type S struct {
	ID     string
	Rail   string
	Routes map[string]string
}

// All are the shipments.
var All = []S{
	{Rail: "Y9"},
	{ID: "2", Routes: map[string]string{"road": "nw-0042"}, Rail: "Y7"},
	{Rail: "Y8", Routes: map[string]string{"road": "se-1111"}},
	{Routes: map[string]string{}, Rail: "Y6"},
	{
		ID:     "3",
		Routes: map[string]string{"road": "sw-2222"},
		Rail:   "Y5",
	},
}
