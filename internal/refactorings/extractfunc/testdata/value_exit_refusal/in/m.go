package m

import "fmt"

type stack struct{ last []string }

func (s *stack) undo(tree string) (string, error) {
	for _, p := range s.last {
		if p != tree {
			return fmt.Sprintf("not in %s", tree), nil
		}
		break
	}
	return "undone", nil
}
