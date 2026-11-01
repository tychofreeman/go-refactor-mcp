package replacetext

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata/replace_text")
}

func TestGoldenTextOnly(t *testing.T) {
	refactortest.Run(t, &ReplaceText{Named: refactor.Called[replaceTextArgs]("replace_text"), TextOnly: true}, "testdata/text_only")
}
