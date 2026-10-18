package p

type ticker interface{ tick() }

func Ticks(v any) bool {
	_, ok := v.(ticker)
	return ok
}
