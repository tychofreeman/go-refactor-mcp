package q

type Sizer interface{ Size() int }

func Describe(v any) string {
	if _, ok := v.(Sizer); ok {
		return "sized"
	}
	return "?"
}
