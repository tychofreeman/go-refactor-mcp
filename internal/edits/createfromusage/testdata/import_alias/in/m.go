package m

import amodels "example.com/m/a/models"

func first(items []*amodels.Item) *amodels.Item {
	var it *amodels.Item = pick(items)
	return it
}
