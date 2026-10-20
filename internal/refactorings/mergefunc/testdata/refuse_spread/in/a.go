package m

func join(xs, ys []any) []any { return append(xs, ys...) }

func nest(xs, ys []any) []any { return append(xs, ys) }

func Use(xs, ys []any) ([]any, []any) { return join(xs, ys), nest(xs, ys) }
