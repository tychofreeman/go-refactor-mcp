package inlinevar

import (
	"testing"

	"github.com/cwfreeman/gorefactor/internal/refactor/refactortest"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}
