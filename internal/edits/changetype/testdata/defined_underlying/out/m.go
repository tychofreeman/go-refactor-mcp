package m

// ID identifies a user.
type ID int64

func next(id ID) ID { return id + 1 }
