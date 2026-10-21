package m

import (
	"testing"

	amodels "example.com/m/a/models"
)

func TestPick(t *testing.T) {
	items := []*amodels.Item{}
	var it *amodels.Item = pick(items)
	_ = it
}
