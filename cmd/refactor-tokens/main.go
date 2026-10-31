// Command refactor-tokens reports where Claude Code sessions spent their
// tokens, by tool, to compare working through the refactoring tools with
// working by hand; and their turns that only loaded schemas or had a call
// refused, and what reading cost by kind.
//
//	refactor-tokens [transcript.jsonl | dir]...
//	refactor-tokens -log .claude/session-log/BRANCH.jsonl
//
// With -log it reads session logs (cmd/session-log) instead: what went
// through gorefactor, what was done by hand, and what was done by hand
// after gorefactor refused, and large writes; then the tokens and turns
// of the logged calls, from the transcripts the log names.
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

var sessionLog = flag.Bool("log", false, "read session logs (cmd/session-log), not transcripts")

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: refactor-tokens [transcript.jsonl | dir]...")
		flag.PrintDefaults()
	}
	flag.Parse()
	log.SetFlags(0)
	args := flag.Args()
	if *sessionLog {
		for _, f := range args {
			w, err := analyzeLog(f)
			if err != nil {
				log.Fatalf("%s: %v", f, err)
			}
			fmt.Print(w.Markdown())
			fmt.Print(transcriptTokens(w))
		}
		return
	}
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

func analyzeLog(path string) (*tokens.WorkReport, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return tokens.AnalyzeWork(f)
}

// transcriptTokens is the tokens and turns of the calls w logged, from the
// session transcripts the log names: only the requests from the first of
// those calls to the last, so a session that did other work before or
// after counts this item alone.
func transcriptTokens(w *tokens.WorkReport) string {
	var sb strings.Builder
	sb.WriteString("\n### Tokens and turns, from the transcript\n\n")
	if len(w.Transcripts) == 0 {
		sb.WriteString("The log names no transcript.\n")
		return sb.String()
	}
	total := &tokens.Report{}
	for _, path := range w.Transcripts {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(&sb, "- not read: %v\n", err)
			continue
		}
		r, err := tokens.AnalyzeOnly(f, w.ToolUseIDs)
		f.Close()
		if err != nil {
			fmt.Fprintf(&sb, "- not read: %s: %v\n", path, err)
			continue
		}
		total.Merge(r)
	}
	sb.WriteString(total.Markdown())
	return sb.String()
}
