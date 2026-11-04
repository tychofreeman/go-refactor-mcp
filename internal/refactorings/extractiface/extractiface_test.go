package extractiface

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestExtractInterface(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}
