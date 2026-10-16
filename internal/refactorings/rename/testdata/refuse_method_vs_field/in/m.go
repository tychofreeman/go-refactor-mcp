package m

type Temp struct{ celsius float64 }

func (t Temp) fahrenheit() float64 { return t.celsius*9/5 + 32 }

func F(t Temp) float64 { return t.fahrenheit() }
