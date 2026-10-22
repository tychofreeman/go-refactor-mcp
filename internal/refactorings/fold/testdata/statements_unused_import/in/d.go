package m

import "log"

func record(sent *[]string, channel, to string) {
	log.Printf("%s to %s", channel, to)
	*sent = append(*sent, to)
}
