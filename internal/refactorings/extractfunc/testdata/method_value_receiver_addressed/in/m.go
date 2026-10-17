package m

import "fmt"

type buf struct{ data []byte }

func (b *buf) reset() { b.data = b.data[:0] }

func (b buf) Show() {
	b.reset()
	b.data = append(b.data, 'x')
	fmt.Println(len(b.data))
}
