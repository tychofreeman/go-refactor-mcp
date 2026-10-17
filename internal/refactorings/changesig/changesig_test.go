package changesig

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGoldenAddParameter(t *testing.T) {
	refactortest.Run(t, NewAddParameter(), "testdata")
}
