package m

type (
	id int
)

func Use(p point) id { return id(p.x) }
