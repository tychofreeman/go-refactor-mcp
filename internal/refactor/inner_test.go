package refactor

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func TestWriteThenRemove(t *testing.T) {
	write := InnerEdit{Inner{Tool: "remove_parameter"}}
	remove := Inner{Tool: "remove_parameter", Schema: json.RawMessage(`{}`)}
	steps, err := WriteThenRemove[*workspace.Workspace](2, write, remove, func(i int) (json.RawMessage, error) {
		return json.Marshal(i)
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range steps {
		_, edit := s.Refactoring.(Edit)
		kind := "remove"
		if edit {
			kind = "write"
		}
		got = append(got, kind+" "+string(s.Args))
	}
	if want := []string{"write 0", "write 1", "remove 0", "remove 1"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if write.Name() != "remove_parameter" || write.Description() != "" || string(remove.InputSchema()) != "{}" {
		t.Errorf("an inner step has the tool's name, no description and the schema given")
	}
}
