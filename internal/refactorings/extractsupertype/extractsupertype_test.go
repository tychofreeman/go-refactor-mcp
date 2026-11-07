package extractsupertype

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestExtractSupertype(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}
