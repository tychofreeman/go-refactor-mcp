package m

// ID identifies a user.
type ID int32

func next(id ID) ID { return id + 1 }
