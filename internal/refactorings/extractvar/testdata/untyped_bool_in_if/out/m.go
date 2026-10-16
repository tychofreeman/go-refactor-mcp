package m

type Cache struct{ items map[string]int }

func (c *Cache) Get(k string) int {
	empty := c.items == nil
	if empty {
		return 0
	}
	return c.items[k]
}
