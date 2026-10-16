package m

type Cache struct{ items map[string]int }

func (c *Cache) Get(k string) int {
	if c.items == nil {
		return 0
	}
	return c.items[k]
}
