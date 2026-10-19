package m

func Scale(n int32) int32 { return n * 2 }

func Run(x int32) int32 { return Scale(x) + Scale(3) }
