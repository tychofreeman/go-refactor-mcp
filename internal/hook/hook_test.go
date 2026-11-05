package hook

import (
	"reflect"
	"testing"
)

// A redirection is a word of its own, so a guard can tell the file a
// command writes from the files it is given; < still counts as a space.
func TestCommandsKeepRedirections(t *testing.T) {
	tests := map[string][][]string{
		"echo x > a.go":            {{"echo", "x", ">", "a.go"}},
		"cat a.go >> b.go; ls":     {{"cat", "a.go", ">>", "b.go"}, {"ls"}},
		"go vet ./... 2>&1 | tail": {{"go", "vet", "./...", "2>&1"}, {"tail"}},
		"cat < plant.go":           {{"cat", "plant.go"}},
		`echo "a > b.go"`:          {{"echo", "a > b.go"}},
	}
	for line, want := range tests {
		if got := Commands(line); !reflect.DeepEqual(got, want) {
			t.Errorf("Commands(%q) = %q, want %q", line, got, want)
		}
	}
}
