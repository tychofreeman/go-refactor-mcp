package m

import "errors"

type file struct{ name string }

func open(path string) (*file, error) {
	if path == "" {
		return nil, errors.New("empty path")
	}
	return &file{path}, nil
}

func read(f *file) []byte { return []byte(f.name) }

func load(path string) ([]byte, error) {
	data, err := openAndRead(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func openAndRead(path string) ([]byte, error) {
	f, err := open(path)
	if err != nil {
		return nil, err
	}
	data := read(f)
	return data, nil
}
