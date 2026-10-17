package changesig

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGoldenAddParameter(t *testing.T) {
	refactortest.Run(t, NewAddParameter(), "testdata/add_parameter")
}

func TestGoldenRemoveParameter(t *testing.T) {
	refactortest.Run(t, NewRemoveParameter(), "testdata/remove_parameter")
}
