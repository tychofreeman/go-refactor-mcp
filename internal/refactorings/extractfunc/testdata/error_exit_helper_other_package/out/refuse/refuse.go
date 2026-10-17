package refuse

import (
	"errors"
	"fmt"
)

type Refusal struct{ Reason string }

func (r Refusal) Error() string { return r.Reason }

// Because returns a Refusal value, which is never nil as an error.
func Because(reason string) error {
	return Refusal{Reason: reason}
}

// Becausef calls Because, or errors.New for an empty format.
func Becausef(format string, args ...any) error {
	if format == "" {
		return errors.New("no reason")
	}
	return Because(fmt.Sprintf(format, args...))
}
