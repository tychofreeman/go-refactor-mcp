// Package report lists users' contact details.
package report

import (
	"fmt"
	"sort"
	"strings"

	"example.com/notify/notify"
)

// Directory is a line per user: name, email and phone.
func Directory(us []notify.User) []string {
	var out []string
	for _, u := range us {
		out = append(out, fmt.Sprintf("%s <%s> %s", u.Name, u.Email, u.Phone))
	}
	return out
}

// MissingEmail lists users without an email address.
func MissingEmail(us []notify.User) []string {
	var out []string
	for _, u := range us {
		if u.Email == "" {
			out = append(out, u.Name)
		}
	}
	return out
}

// MissingPhone lists users without a phone number.
func MissingPhone(us []notify.User) []string {
	var out []string
	for _, u := range us {
		if u.Phone == "" {
			out = append(out, u.Name)
		}
	}
	return out
}

// EmailDomain is the domain of u's email address.
func EmailDomain(u notify.User) string {
	i := strings.LastIndex(u.Email, "@")
	if i < 0 {
		return ""
	}
	return u.Email[i+1:]
}

// SameEmail reports whether a and b have the same address.
func SameEmail(a, b notify.User) bool { return strings.EqualFold(a.Email, b.Email) }

// PhoneDigits is u's phone number without what is not a digit.
func PhoneDigits(u notify.User) string {
	var b strings.Builder
	for _, r := range u.Phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ByEmail indexes users by email address.
func ByEmail(us []notify.User) map[string]notify.User {
	m := map[string]notify.User{}
	for _, u := range us {
		m[u.Email] = u
	}
	return m
}

// CSV lists users as "id,name,email,phone" lines, by ID.
func CSV(us []notify.User) string {
	sorted := append([]notify.User(nil), us...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var b strings.Builder
	for _, u := range sorted {
		fmt.Fprintf(&b, "%s,%s,%s,%s\n", u.ID, u.Name, u.Email, u.Phone)
	}
	return b.String()
}

// Contactable reports whether u can be reached by email or phone.
func Contactable(u notify.User) bool { return u.Email != "" || u.Phone != "" }

// Label is how a user is shown: name and the first contact there is.
func Label(u notify.User) string {
	switch {
	case u.Email != "":
		return u.Name + " (" + u.Email + ")"
	case u.Phone != "":
		return u.Name + " (" + u.Phone + ")"
	}
	return u.Name
}

// Domains counts users by email domain.
func Domains(us []notify.User) map[string]int {
	m := map[string]int{}
	for _, u := range us {
		m[EmailDomain(u)]++
	}
	return m
}
