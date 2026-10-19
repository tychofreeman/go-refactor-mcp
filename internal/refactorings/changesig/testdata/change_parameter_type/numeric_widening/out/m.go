package m

func Scale(n int64) int32 { return int32(n) * 2 }

func Run(x int32) int32 { return Scale(int64(x)) + Scale(3) }
