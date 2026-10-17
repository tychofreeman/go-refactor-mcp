package changesig

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGoldenIntroduceParameter(t *testing.T) {
	refactortest.Run(t, NewIntroduceParameter(), "testdata/introduce_parameter")
}
