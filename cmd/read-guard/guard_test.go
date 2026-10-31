package main

import (
	"strings"
	"testing"
)

func TestRefusal(t *testing.T) {
	read := func(path string) call {
		var c call
		c.ToolName, c.ToolInput.FilePath = "Read", path
		return c
	}
	bash := func(cmd string) call {
		var c call
		c.ToolName, c.ToolInput.Command = "Bash", cmd
		return c
	}
	refused := []call{
		read("/home/u/m/internal/plant/plant.go"),
		read("bed_test.go"),
		bash("sed -n 1,20p internal/plant/plant.go"),
		bash("cd /home/u/m && cat plant.go | head -5"),
		bash("grep -n Water internal/plant/plant.go"),
		bash("/usr/bin/head -5 bed_test.go"),
		bash("echo $(cat plant.go)"),
		bash("tail -n +3 'my plant.go'"),
		bash("awk '{print}' plant.go"),
		bash("cat < plant.go"),
		bash("git status; sed -n 5,9p a.go"),
		bash("LC_ALL=C grep -c x plant.go"),
	}
	for _, c := range refused {
		if refusal(c) == "" {
			t.Errorf("%+v was allowed", c.ToolInput)
		}
	}
	allowed := []call{
		read("docs/roadmap.md"),
		read("internal/plant/testdata/water/in/plant.go"),
		bash("gofmt -l $(git ls-files '*.go' | grep -v /testdata/)"),
		bash(`grep -rn "plant.go" docs`),
		bash("grep -e Water -rn internal"),
		bash("git diff internal/plant/plant.go"),
		bash("go test ./..."),
		bash("sed -n 1,5p docs/roadmap.md"),
		bash("cat internal/plant/testdata/water/args.json"),
		bash(`echo "cat plant.go"`),
	}
	for _, c := range allowed {
		if why := refusal(c); why != "" {
			t.Errorf("%+v was refused: %s", c.ToolInput, why)
		}
	}
}

// A path is judged where it points: after the cds before it, from the
// hook's working directory, with variables the line set expanded.
func TestRefusalResolvesPaths(t *testing.T) {
	bash := func(cwd, cmd string) call {
		var c call
		c.Cwd, c.ToolName, c.ToolInput.Command = cwd, "Bash", cmd
		return c
	}
	refused := []call{
		bash("/home/u/m", "cd internal/plant && cat plant.go"),
		bash("/home/u/m", "cd internal/plant/testdata/.. && cat plant.go"),
		bash("/home/u/m", "d=internal/plant; cat $d/plant.go"),
		bash("/home/u/m/internal/plant", "sed -n 1,9p plant.go"),
	}
	for _, c := range refused {
		if refusal(c) == "" {
			t.Errorf("%q in %s was allowed", c.ToolInput.Command, c.Cwd)
		}
	}
	allowed := []call{
		bash("/home/u/m/internal/plant/testdata/water", "cat in/plant.go"),
		bash("/home/u/m", "cd internal/plant/testdata/water && cat in/plant.go"),
		bash("/home/u/m", "d=/home/u/m/internal/plant/testdata/water; cat $d/in/plant.go"),
		bash("/home/u/m", "d=internal/plant/testdata; cat ${d}/water/in/plant.go"),
	}
	for _, c := range allowed {
		if why := refusal(c); why != "" {
			t.Errorf("%q in %s was refused: %s", c.ToolInput.Command, c.Cwd, why)
		}
	}
}

// The CLI is named only where it is installed.
func TestAdviceNamesTheCLIOnlyWhereItIs(t *testing.T) {
	if strings.Contains(adviceFor(false), "refactor inspect") {
		t.Errorf("advice without the CLI names it: %s", adviceFor(false))
	}
	if !strings.Contains(adviceFor(true), "refactor inspect") {
		t.Errorf("advice with the CLI does not name it: %s", adviceFor(true))
	}
}
