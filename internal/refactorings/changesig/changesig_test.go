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

func TestGoldenMoveParameter(t *testing.T) {
	refactortest.Run(t, NewMoveParameter(), "testdata/move_parameter")
}

func TestGoldenChangeFieldType(t *testing.T) {
	refactortest.Run(t, NewChangeFieldType(), "testdata/change_field_type")
}

func TestGoldenAddResult(t *testing.T) {
	refactortest.Run(t, NewAddResult(), "testdata/add_result")
}

func TestGoldenChangeParameterType(t *testing.T) {
	refactortest.Run(t, NewChangeParameterType(), "testdata/change_parameter_type")
}
