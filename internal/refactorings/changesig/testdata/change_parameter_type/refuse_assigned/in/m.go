package m

func Clip(n int32) int32 {
	if n > 9 {
		n = 9
	}
	return n
}

func Run() int32 { return Clip(12) }
