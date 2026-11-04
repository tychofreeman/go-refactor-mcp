package split

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestVariable(t *testing.T) {
	refactortest.Run(t, New(), "testdata/variable")
}

func TestLoop(t *testing.T) {
	refactortest.Run(t, New(), "testdata/loop")
}

func TestPhase(t *testing.T) {
	refactortest.Run(t, New(), "testdata/phase")
}
