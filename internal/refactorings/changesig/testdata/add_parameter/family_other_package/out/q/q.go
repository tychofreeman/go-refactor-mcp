package q

type Sizer interface{ Size(unit string) int }

func Describe(v any) string {
	if _, ok := v.(Sizer); ok {
		return "sized"
	}
	return "?"
}
