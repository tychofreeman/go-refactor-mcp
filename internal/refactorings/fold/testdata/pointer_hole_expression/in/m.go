package m

type Cart struct{ Items []string }

func count(items *[]string) int { return len(*items) + 1 }

func Size(c *Cart) int { return len(c.Items) + 1 }
