package files

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGoldenMove(t *testing.T) {
	refactortest.Run(t, NewMove(), "testdata/move")
}

func TestGoldenDelete(t *testing.T) {
	refactortest.Run(t, NewDelete(), "testdata/delete")
}
