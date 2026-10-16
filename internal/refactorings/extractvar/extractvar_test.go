package extractvar

import (
	"testing"

	"github.com/cwfreeman/gorefactor/internal/refactor/refactortest"
)

func TestGolden(t *testing.T) {
	refactortest.Run(t, New(), "testdata")
}

func TestTokens(t *testing.T) {
	a, err := tokens([]byte("a+b*\n\tc"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := tokens([]byte("a + b * c"))
	if !equalTokens(a, b) {
		t.Errorf("whitespace should not matter: %q vs %q", a, b)
	}
	c, _ := tokens([]byte(`"a b"`))
	d, _ := tokens([]byte(`"ab"`))
	if equalTokens(c, d) {
		t.Error("whitespace inside string literals must matter")
	}
}
