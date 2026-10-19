package gocode

import "strings"

// CommentWidth returns the width the file src wraps its comments at: its
// longest top-level "// " comment line, leaving out lines over 100
// columns, or 76 for a file with fewer than five such lines.
func CommentWidth(src []byte) int {
	width, n := 0, 0
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.HasPrefix(line, "// ") || len(line) > 100 {
			continue
		}
		n++
		width = max(width, len(line))
	}
	if n < 5 {
		return 76
	}
	return width
}

// DocComment returns doc as a comment for the line above a top-level
// declaration: each line starts with "// " unless it is a comment already,
// and a line longer than width is wrapped at spaces. Shorter lines, line
// breaks and indented lines (code in the comment) are kept. An empty doc
// gives no comment.
func DocComment(doc string, width int) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	var sb strings.Builder
	for _, line := range strings.Split(doc, "\n") {
		line = strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			line = strings.TrimPrefix(strings.TrimPrefix(trimmed, "//"), " ")
		}
		switch {
		case len(trimmed) > 2 && strings.HasPrefix(trimmed, "//") && !strings.ContainsAny(trimmed[2:3], " \t"):
			sb.WriteString(trimmed + "\n") // a directive, such as //go:generate
		case line == "":
			sb.WriteString("//\n")
		case line[0] == '\t' || line[0] == ' ':
			sb.WriteString("//" + line + "\n")
		default:
			cur := "//"
			for _, word := range strings.Fields(line) {
				if cur != "//" && len(cur)+1+len(word) > width {
					sb.WriteString(cur + "\n")
					cur = "//"
				}
				cur += " " + word
			}
			sb.WriteString(cur + "\n")
		}
	}
	return sb.String()
}
