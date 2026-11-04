package m

// Tray holds one thing.
type Tray[T any] struct{ v T }

// Get returns what it holds.
func (t Tray[T]) Get() T { return t.v }
