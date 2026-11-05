package main

import (
	"testing"

	"github.com/tychofreeman/go-refactor-mcp/internal/hook"
)

func TestRefusal(t *testing.T) {
	edit := func(tool, path string) hook.Call {
		var c hook.Call
		c.ToolName, c.ToolInput.FilePath = tool, path
		return c
	}
	bash := func(cmd string) hook.Call {
		var c hook.Call
		c.ToolName, c.ToolInput.Command = "Bash", cmd
		return c
	}
	refused := []hook.Call{
		edit("Edit", "/home/u/m/internal/plant/plant.go"),
		edit("Write", "bed_test.go"),
		edit("MultiEdit", "internal/plant/plant.go"),
		bash("sed -i 's/Water/Soak/' internal/plant/plant.go"),
		bash("sed -i.bak -e s/a/b/ plant.go"),
		bash("perl -pi -e 's/a/b/' plant.go"),
		bash("echo 'package plant' > plant.go"),
		bash("cat head.txt >> plant.go"),
		bash("printf x | tee plant.go"),
		bash("cp /tmp/plant.go internal/plant/plant.go"),
		bash("mv plant.go garden.go"),
		bash("rm internal/plant/plant.go"),
		bash("cd internal/plant && echo x > plant.go"),
	}
	for _, c := range refused {
		if refusal(c) == "" {
			t.Errorf("%s %+v was allowed", c.ToolName, c.ToolInput)
		}
	}
	allowed := []hook.Call{
		edit("Edit", "docs/roadmap.md"),
		edit("Write", "internal/plant/testdata/water/out/plant.go"),
		bash("go test ./... > /tmp/out.txt 2>&1"),
		bash("gofmt -w internal/plant/plant.go"),
		bash("sed -n 1,5p docs/roadmap.md"),
		bash("sed 's/a/b/' plant.go > /tmp/plant.txt"),
		bash("cp internal/plant/plant.go /tmp/plant.go.orig"),
		bash(`echo "x > plant.go"`),
		bash("git checkout main"),
	}
	for _, c := range allowed {
		if why := refusal(c); why != "" {
			t.Errorf("%s %+v was refused: %s", c.ToolName, c.ToolInput, why)
		}
	}
}
