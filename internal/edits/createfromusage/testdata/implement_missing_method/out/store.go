package m

type Store interface {
	Get(key string) (string, bool)
	Put(key, value string)
}

type mem struct{ m map[string]string }

func (s *mem) Get(key string) (string, bool) { v, ok := s.m[key]; return v, ok }

func (s *mem) Put(key string, value string) {
	panic("not implemented")
}
