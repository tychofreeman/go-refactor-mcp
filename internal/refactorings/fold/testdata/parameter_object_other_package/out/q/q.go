package q

import "example.com/m/p"

func Run(us []string) int { return p.Send2(p.Batch{Users: us, Message: "hi"}) }
