// Package alerts raises alerts when a sensor in the warehouse reads too high.
package alerts

import "fmt"

// Temp is a temperature sensor.
type Temp struct {
	Room  string
	Limit float64
}

// Humidity is a humidity sensor.
type Humidity struct {
	Room  string
	Limit float64
}

// Smoke is a smoke sensor.
type Smoke struct {
	Room  string
	Limit float64
}

// Load is a sensor of the weight on a rack.
type Load struct {
	Room  string
	Limit float64
}

// Noise is a sound level sensor.
type Noise struct {
	Room  string
	Limit float64
}

// Dust is a dust sensor.
type Dust struct {
	Room  string
	Limit float64
}

// Gas is a carbon monoxide sensor.
type Gas struct {
	Room  string
	Limit float64
}

// Vibration is a sensor on a conveyor motor.
type Vibration struct {
	Room  string
	Limit float64
}

// Light is a light level sensor.
type Light struct {
	Room  string
	Limit float64
}

// Flood is a flood sensor.
type Flood struct {
	Room  string
	Limit float64
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Temp) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: temp %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Humidity) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: humidity %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Smoke) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: smoke %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Load) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: load %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Noise) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: noise %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Dust) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: dust %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Gas) Probe(v float64, alerts *[]string) {
	if v > s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: gas %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Vibration) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: vibration %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Light) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: light %.1f over %.1f", s.Room, v, s.Limit))
	}
}

// Probe adds the alert for reading v, if there is one, to alerts.
func (s Flood) Probe(v float64, alerts *[]string) {
	if v >= s.Limit {
		*alerts = append(*alerts, fmt.Sprintf("%s: flood %.1f over %.1f", s.Room, v, s.Limit))
	}
}
