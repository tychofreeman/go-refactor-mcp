package m

import "fmt"

type stack struct{ last []string }

func (s *stack) undo(tree string) (string, error) {
	if done, v, err := s.elsewhere(tree); done {
		return v, err
	}
	return "undone", nil
}

func (s *stack) elsewhere(tree string) (bool, string, error) {
	for _, p := range s.last {
		if p != tree {
			return true, fmt.Sprintf("not in %s", tree), nil
		}
		break
	}
	return false, "", nil
}
