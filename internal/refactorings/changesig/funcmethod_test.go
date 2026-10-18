package changesig

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGoldenFunctionToMethod(t *testing.T) {
	refactortest.Run(t, NewFunctionToMethod(), "testdata/function_to_method")
}
