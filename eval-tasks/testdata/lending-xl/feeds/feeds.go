// Package feeds loads the catalog from the library's suppliers.
package feeds

import (
	"strings"

	"example.com/lending/lending"
)

// Novels loads the novels feed into c.
func Novels(c *lending.Catalog) error {
	return lending.Import(c, "n1|Emma|book|2\nn2|Dracula|book|1")
}

// Films loads the films feed into c.
func Films(c *lending.Catalog) error {
	return lending.Import(c, "f1|Heat|dvd|3\nf2|Alien|dvd|1")
}

// Maps loads the maps feed into c.
func Maps(c *lending.Catalog) error {
	return lending.Import(c, "m1|World|atlas|1")
}

// Poetry loads the poetry feed into c.
func Poetry(c *lending.Catalog) error {
	return lending.Import(c, "p1|Odes|book|1\np2|Psalms|book|2")
}

// History loads the history feed into c.
func History(c *lending.Catalog) error {
	return lending.Import(c, "h1|Rome|book|2\n# atlases\nh2|Empires|atlas|1")
}

// Science loads the science feed into c.
func Science(c *lending.Catalog) error {
	return lending.Import(c, "s1|Cosmos|dvd|1\ns2|Origins|book|1")
}

// Children loads the children feed into c.
func Children(c *lending.Catalog) error {
	return lending.Import(c, "c1|Matilda|book|4")
}

// Music loads the music feed into c.
func Music(c *lending.Catalog) error {
	return lending.Import(c, "u1|Jazz|dvd|2")
}

// Travel loads the travel feed into c.
func Travel(c *lending.Catalog) error {
	return lending.Import(c, "t1|Alps|atlas|1\nt2|Coasts|book|1")
}

// Archive loads the archive feed into c.
func Archive(c *lending.Catalog) error {
	return lending.Import(c, "a1|Ledger|book|1")
}

// Preview lists the titles of text's well-formed lines, without adding
// anything.
func Preview(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) == 4 {
			out = append(out, strings.TrimSpace(f[1]))
		}
	}
	return out
}
