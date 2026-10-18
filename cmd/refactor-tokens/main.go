// Command refactor-tokens reports where Claude Code sessions spent their
// tokens, by tool, to compare working through the refactoring tools with
// working by hand.
//
//	refactor-tokens [transcript.jsonl | dir]...
//
// With no arguments it reads every session of the project in the current
// directory (~/.claude/projects/<project>/*.jsonl).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/tokens"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: refactor-tokens [transcript.jsonl | dir]...")
		flag.PrintDefaults()
	}
	flag.Parse()
	log.SetFlags(0)
	args := flag.Args()
	if len(args) == 0 {
		dir, err := projectDir()
		if err != nil {
			log.Fatal(err)
		}
		args = []string{dir}
	}
	files, err := transcripts(args)
	if err != nil {
		log.Fatal(err)
	}
	total := &tokens.Report{}
	for _, f := range files {
		r, err := analyzeFile(f)
		if err != nil {
			log.Fatalf("%s: %v", f, err)
		}
		total.Merge(r)
	}
	fmt.Printf("- sessions: %d\n%s", len(files), total.Markdown())
}

// projectDir is where Claude Code keeps the current directory's sessions.
func projectDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	name := strings.NewReplacer("/", "-", ".", "-").Replace(wd)
	return filepath.Join(home, ".claude", "projects", name), nil
}

func transcripts(args []string) ([]string, error) {
	var files []string
	for _, a := range args {
		fi, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			files = append(files, a)
			continue
		}
		m, err := filepath.Glob(filepath.Join(a, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	return files, nil
}

func analyzeFile(path string) (*tokens.Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return tokens.Analyze(f)
}
