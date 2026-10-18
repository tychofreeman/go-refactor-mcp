package slidestmt

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestUp(t *testing.T) {
	refactortest.Run(t, NewUp(), "testdata/up")
}

func TestDown(t *testing.T) {
	refactortest.Run(t, NewDown(), "testdata/down")
}
