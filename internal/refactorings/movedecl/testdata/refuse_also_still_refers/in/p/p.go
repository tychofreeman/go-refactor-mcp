package p

func one() int { return 1 }

func two() int { return one() + 1 }

func Three() int { return two() + 1 }
