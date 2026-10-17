package m

import "fmt"

type refusal struct{ reason string }

func (r *refusal) Error() string { return r.reason }

// refusef always returns a non-nil error.
func refusef(format string, args ...any) error {
	return &refusal{reason: fmt.Sprintf(format, args...)}
}

func plan(name string) (string, error) {
	fmt.Println("planning")
	if name == "" {
		return "", refusef("empty name")
	}
	fmt.Println("name ok")
	return name + "!", nil
}
