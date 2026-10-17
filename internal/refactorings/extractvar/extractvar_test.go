package extractvar

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/locate"
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor/refactortest"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}

func TestTokens(t *testing.T) {
	a, err := locate.Tokens([]byte("a+b*\n\tc"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := locate.Tokens([]byte("a + b * c"))
	if !locate.EqualTokens(a, b) {
		t.Errorf("whitespace should not matter: %q vs %q", a, b)
	}
	c, _ := locate.Tokens([]byte(`"a b"`))
	d, _ := locate.Tokens([]byte(`"ab"`))
	if locate.EqualTokens(c, d) {
		t.Error("whitespace inside string literals must matter")
	}
}
