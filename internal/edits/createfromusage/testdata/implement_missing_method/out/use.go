package m

func Use(s Store) {}

func Run() { Use(&mem{}) }
