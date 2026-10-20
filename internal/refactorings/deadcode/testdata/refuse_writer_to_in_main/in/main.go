package main

import (
	"io"
	"os"
)

type src struct{ n int }

func (s *src) Read(p []byte) (int, error) {
	if s.n > 0 {
		return 0, io.EOF
	}
	s.n++
	return copy(p, "read"), nil
}

// WriteTo writes everything at once.
func (s *src) WriteTo(w io.Writer) (int64, error) {
	n, err := io.WriteString(w, "written")
	return int64(n), err
}

func main() { io.Copy(os.Stdout, &src{}) }
