package m

import "strings"

func parse(line string) any {
	split := strings.Split(line, " ")
	return entry{hash: split[0], date: len(split)}
}
