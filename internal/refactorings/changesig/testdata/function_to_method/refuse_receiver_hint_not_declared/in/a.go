package m

// Fern is a species.
type Fern struct{}

func fernLabel(name string, f Fern) string { return name + " (fern)" }

// Tag labels a fern.
func Tag(name string) string { return fernLabel(name, Fern{}) }
