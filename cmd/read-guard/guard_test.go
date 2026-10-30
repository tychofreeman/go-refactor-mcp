package main

import "testing"

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
