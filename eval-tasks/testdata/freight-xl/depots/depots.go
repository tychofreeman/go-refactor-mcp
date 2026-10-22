// Package depots ships each depot's orders.
package depots

import "example.com/freight/freight"

// North ships o from the north depot to shipments that have a road code.
func North(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "N" + o.Ref
	return freight.Dispatch(c, road, o)
}

// South ships o from the south depot to shipments that have a road code.
func South(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "S" + o.Ref
	return freight.Dispatch(c, road, o)
}

// East ships o from the east depot to shipments that have a road code.
func East(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "E" + o.Ref
	return freight.Dispatch(c, road, o)
}

// West ships o from the west depot to shipments that have a road code.
func West(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "W" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Central ships o from the central depot to shipments that have a road code.
func Central(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "C" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Harbor ships o from the harbor depot to shipments that have a road code.
func Harbor(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "H" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Airport ships o from the airport depot to shipments that have a road code.
func Airport(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "A" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Border ships o from the border depot to shipments that have a road code.
func Border(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "B" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Valley ships o from the valley depot to shipments that have a road code.
func Valley(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "V" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Coast ships o from the coast depot to shipments that have a road code.
func Coast(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "K" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Hill ships o from the hill depot to shipments that have a road code.
func Hill(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "L" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Lake ships o from the lake depot to shipments that have a road code.
func Lake(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "Q" + o.Ref
	return freight.Dispatch(c, road, o)
}

// River ships o from the river depot to shipments that have a road code.
func River(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "R" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Plain ships o from the plain depot to shipments that have a road code.
func Plain(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "P" + o.Ref
	return freight.Dispatch(c, road, o)
}

// Forest ships o from the forest depot to shipments that have a road code.
func Forest(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {
	var road []freight.Shipment
	for _, s := range shipments {
		if s.Road != "" {
			road = append(road, s)
		}
	}
	o.Ref = "F" + o.Ref
	return freight.Dispatch(c, road, o)
}
