package m

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

func (s *src) writeAll(w io.Writer) (int64, error) {
	n, err := io.WriteString(w, "written")
	return int64(n), err
}

func Copy() { io.Copy(os.Stdout, &src{}); (&src{}).writeAll(os.Stdout) }
