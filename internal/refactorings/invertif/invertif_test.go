package invertif

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata/invert_if")
}

func TestGoldenRemoveElse(t *testing.T) {
	refactortest.Run(t, NewRemoveElse(), "testdata/remove_else")
}
