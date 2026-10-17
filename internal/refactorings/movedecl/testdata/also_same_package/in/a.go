package m

func first() int { return second() + Limit }

func keep() int { return first() }

func second() int { return 2 }

const Limit = 3
