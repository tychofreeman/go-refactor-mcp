// Command refactor-mcp is an MCP server (stdio) that gives an LLM
// refactoring and editing tools for one Go module.
//
// The module is the one containing, in order of precedence: the -C flag,
// $CLAUDE_PROJECT_DIR (set by Claude Code for the servers it starts), or
// the current directory.
//
//	claude mcp add gorefactor -- refactor-mcp
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tychofreeman/go-refactor-mcp/internal/mcpserver"
)

var fullDiffs = flag.Bool("full-diffs", false, "show results with three lines of context around each change (default: none)")

var readByName = flag.Bool("read-by-name", false, "inspect shows source only for what a request names (function, names, uses), not lines by number")

var noCodeByText = flag.Bool("no-code-by-text", false, "replace_text changes a Go file only inside string literals and comments; code is changed by the structured edits")

func main() {
	dir := flag.String("C", "", "module directory (default: $CLAUDE_PROJECT_DIR, else the current directory)")
	pattern := flag.String("p", "./...", "package pattern to load, relative to the module root")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *version {
		fmt.Println("refactor-mcp", mcpserver.BuildVersion())
		return
	}
	if *dir == "" {
		*dir = os.Getenv("CLAUDE_PROJECT_DIR")
	}
	if *dir == "" {
		*dir = "."
	}
	if fi, err := os.Stat(*dir); err != nil || !fi.IsDir() {
		log.Fatalf("refactor-mcp: %s is not a directory", *dir)
	}

	// stdout carries the protocol; logs go to stderr.
	log.SetOutput(os.Stderr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s := mcpserver.New(*dir, *pattern)
	s.FullDiffs = *fullDiffs
	s.ReadByName = *readByName
	s.NoCodeByText = *noCodeByText
	s.Preload()
	srv := s.MCP()
	err := srv.Run(ctx, &mcp.StdioTransport{})
	// The client closing stdin (EOF) is the normal way a session ends.
	if err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) && !errors.Is(err, mcp.ErrConnectionClosed) {
		log.Fatalf("refactor-mcp: %v", err)
	}
}
