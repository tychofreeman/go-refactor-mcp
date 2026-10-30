package workspace

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Uses renders, for each of names ("Name" or "Class.member"), every use
// in the solution of what it names, found by the type checker, under what
// is done with it; a use two projects share is listed once.
func (w *Workspace) Uses(names []string) string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = w.usesOf(name)
	}
	return strings.Join(out, "\n")
}

// usesOf renders the uses of name.
func (w *Workspace) usesOf(name string) string {
	desc, found := "", false
	seen := map[string]bool{}
	byRole := map[string][]tsapi.Use{}
	n := 0
	for _, p := range w.projects {
		d, us, ok, why := p.UsesOf(context.Background(), name, w.Rel)
		if why != "" {
			return fmt.Sprintf("%s: %s\n", name, why)
		}
		if !ok {
			continue
		}
		desc, found = d, true
		for _, u := range us {
			if k := fmt.Sprint(u.File, ":", u.Pos); !seen[k] {
				seen[k] = true
				byRole[u.Role] = append(byRole[u.Role], u)
				n++
			}
		}
	}
	if !found {
		return name + ": no declaration of that name in the solution\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s): ", name, desc)
	if n == 0 {
		b.WriteString("no uses\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d uses\n", n)
	for _, role := range tsapi.UseRoles {
		us := byRole[role]
		if len(us) == 0 {
			continue
		}
		sort.Slice(us, func(i, j int) bool {
			if us[i].File != us[j].File {
				return us[i].File < us[j].File
			}
			return us[i].Pos < us[j].Pos
		})
		fmt.Fprintf(&b, "%s (%d):\n", role, len(us))
		for _, u := range us {
			fmt.Fprintf(&b, "  %s:%d  %s\n", w.Rel(u.File), u.Line, u.Text)
		}
	}
	return b.String()
}
