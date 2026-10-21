package q

import "example.com/m"

func Use() int { return m.Run(2, m.Batch{N: 1, Message: m.Message{}}) }
