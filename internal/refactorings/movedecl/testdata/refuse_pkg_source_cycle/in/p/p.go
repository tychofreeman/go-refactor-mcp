package p

func Base() int { return 1 }

func Two() int { return Base() + 1 }

func Three() int { return Two() + 1 }
