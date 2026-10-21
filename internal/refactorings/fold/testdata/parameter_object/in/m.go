package m

import "strings"

// Batch is a message and who to send it to.
type Batch struct {
	Users   []string
	Message string
	Urgent  bool
}

func Send(prefix string, users []string, m string) string {
	return prefix + strings.Join(users, ",") + ": " + m
}

func Send2(prefix string, b Batch) string { return Send(prefix, b.Users, b.Message) }

func a(us []string) string { return Send("to ", us, "hi") }

func c(us []string, m string) string { return Send(">", us[1:], m+"!") }
