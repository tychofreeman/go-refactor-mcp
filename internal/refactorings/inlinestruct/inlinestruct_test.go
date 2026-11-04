package inlinestruct

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestInlineStruct(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}
