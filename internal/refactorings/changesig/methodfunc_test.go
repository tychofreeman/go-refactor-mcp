package changesig

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGoldenMethodToFunction(t *testing.T) {
	refactortest.Run(t, NewMethodToFunction(), "testdata/method_to_function")
}
