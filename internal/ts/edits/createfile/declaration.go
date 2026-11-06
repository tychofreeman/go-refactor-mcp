package createfile

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// classHeader is a class's header: its name, the class it extends and the
// interfaces it implements, without type arguments.
var classHeader = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)(?:\s+extends\s+([A-Za-z_$][\w$]*))?(?:\s+implements\s+([A-Za-z_$][\w$]*(?:\s*,\s*[A-Za-z_$][\w$]*)*))?$`)

// otherHeader is an interface's or an enum's header, which gets an empty
// body.
var otherHeader = regexp.MustCompile(`^(?:export\s+)?(?:declare\s+)?(?:const\s+)?(?:interface|enum)\s+[A-Za-z_$][\w$]*(?:<[^{}]*>)?(?:\s+extends\s+[^{}]+)?$`)

// fromDeclaration returns the file made from a class's, interface's or
// enum's header: the declaration with a body that holds, for a class, a
// member for each that the interfaces it implements and the abstract class
// it extends require.
func fromDeclaration(ws *workspace.Workspace, declaration string) (string, error) {
	header := strings.TrimSpace(declaration)
	if otherHeader.MatchString(header) {
		return header + " {}\n", nil
	}
	m := classHeader.FindStringSubmatch(header)
	if m == nil {
		return "", refactor.Refusef("declaration is the header of a class, an interface or an enum, without a body or type arguments, such as \"export class Bed extends Soil implements Planter\"; to write anything else, give content")
	}
	var implements []string
	for _, name := range strings.Split(m[3], ",") {
		if name = strings.TrimSpace(name); name != "" {
			implements = append(implements, name)
		}
	}
	members, why := ws.ClassMembers(implements, m[2])
	if why != "" {
		return "", refactor.Refusef("%s", why)
	}
	if len(members) == 0 {
		return header + " {}\n", nil
	}
	return header + " {\n  " + strings.Join(members, "\n\n  ") + "\n}\n", nil
}

// Skeleton is create_file for a server that creates structure by command:
// TypeScript content with a function or method body is refused, while
// classes with fields, interfaces, types and what declaration and
// test_for write pass.
type Skeleton struct{ *CreateFile }

// Plan refuses content with a body, else plans as create_file does.
func (s Skeleton) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a createFileArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if ws.Owns(a.File) && a.Content != "" {
		if name := tsapi.Bodied(a.Content); name != "" {
			return nil, refactor.Refusef("this server creates code by structure: content may hold classes with fields, interfaces and types, not %s's body. Make the class with declaration (implements and extends give its stubs), a test with test_for, and each function or method with add_declaration", name)
		}
	}
	return s.CreateFile.Plan(ws, raw)
}
