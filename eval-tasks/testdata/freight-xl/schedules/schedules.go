// Package schedules makes the regular shipping runs.
package schedules

import "example.com/freight/freight"

// Daily ships the daily order to shipments.
func Daily(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "DAY-1", Weight: 10})
}

// Weekly ships the weekly order to shipments.
func Weekly(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "WEEK-2", Weight: 20})
}

// Monthly ships the monthly order to shipments.
func Monthly(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "MONTH-3", Weight: 30})
}

// Express ships the express order to shipments.
func Express(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "EXP-4", Weight: 40})
}

// Overnight ships the overnight order to shipments.
func Overnight(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "NIGHT-5", Weight: 50})
}

// Bulk ships the bulk order to shipments.
func Bulk(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "BULK-6", Weight: 60})
}

// Fragile ships the fragile order to shipments.
func Fragile(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "FRAG-7", Weight: 70})
}

// Cold ships the cold order to shipments.
func Cold(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "COLD-8", Weight: 80})
}

// Hazard ships the hazard order to shipments.
func Hazard(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "HAZ-9", Weight: 90})
}

// Returns ships the returns order to shipments.
func Returns(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "RET-10", Weight: 100})
}

// Samples ships the samples order to shipments.
func Samples(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "SAMP-11", Weight: 110})
}

// Parts ships the parts order to shipments.
func Parts(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "PART-12", Weight: 120})
}

// Retail ships the retail order to shipments.
func Retail(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "RETL-13", Weight: 130})
}

// Wholesale ships the wholesale order to shipments.
func Wholesale(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "WHOL-14", Weight: 140})
}

// Archive ships the archive order to shipments.
func Archive(c freight.Config, shipments []freight.Shipment) []string {
	return freight.Dispatch(c, shipments, freight.Order{Ref: "ARCH-15", Weight: 150})
}
