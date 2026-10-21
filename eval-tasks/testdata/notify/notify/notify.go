// Package notify sends messages to users by email and text message.
package notify

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// User is someone to notify.
type User struct {
	ID    string
	Name  string
	Email string
	Phone string
}

// Message is what is sent.
type Message struct {
	Subject string
	Body    string
}

// Notifier sends a message to a user on one channel.
type Notifier interface {
	Send(u User, m Message)
	Name() string
}

// Email sends by email.
type Email struct {
	Sent []string // what was sent, "address: subject"
}

// Send sends m to u's email address.
func (e *Email) Send(u User, m Message) {
	log.Printf("email to %s", u.Email)
	e.Sent = append(e.Sent, u.Email+": "+m.Subject)
}

// Name is the channel's name.
func (e *Email) Name() string { return "email" }

// SMS sends by text message, which holds the subject only, cut to 20
// characters.
type SMS struct {
	Sent []string // what was sent, "phone: subject"
}

// Send sends m to u's phone.
func (s *SMS) Send(u User, m Message) {
	log.Printf("sms to %s", u.Phone)
	s.Sent = append(s.Sent, u.Phone+": "+truncate(m.Subject, 20))
}

// Name is the channel's name.
func (s *SMS) Name() string { return "sms" }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Config says how Broadcast sends.
type Config struct {
	Channels []Notifier
}

// Broadcast sends m to each user on every channel, in the order given,
// and returns a summary line for each user: "name: channel,channel".
func Broadcast(c Config, users []User, m Message) []string {
	var out []string
	for _, u := range users {
		var names []string
		for _, ch := range c.Channels {
			ch.Send(u, m)
			names = append(names, ch.Name())
		}
		out = append(out, fmt.Sprintf("%s: %s", u.Name, strings.Join(names, ",")))
	}
	return out
}

// SortUsers sorts users in place by less.
func SortUsers(users []User, less func(a, b User) bool) {
	sort.SliceStable(users, func(i, j int) bool { return less(users[i], users[j]) })
}
