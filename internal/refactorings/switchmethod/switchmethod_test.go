package switchmethod

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata/type_switch_to_method")
}

func TestGoldenPrepareForTypes(t *testing.T) {
	refactortest.Run(t, NewPrepareForTypes(), "testdata/prepare_switch_for_types")
}
