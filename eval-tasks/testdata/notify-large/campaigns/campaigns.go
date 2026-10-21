// Package campaigns sends the shop's regular messages.
package campaigns

import "example.com/notify/notify"

// Welcome sends the welcome message to users.
func Welcome(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Welcome news", Body: "About welcome."})
}

// Reset sends the reset message to users.
func Reset(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Reset news", Body: "About reset."})
}

// Weekly sends the weekly message to users.
func Weekly(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Weekly news", Body: "About weekly."})
}

// Promo sends the promo message to users.
func Promo(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Promo news", Body: "About promo."})
}

// Outage sends the outage message to users.
func Outage(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Outage news", Body: "About outage."})
}

// Survey sends the survey message to users.
func Survey(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Survey news", Body: "About survey."})
}

// Renewal sends the renewal message to users.
func Renewal(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Renewal news", Body: "About renewal."})
}

// Goodbye sends the goodbye message to users.
func Goodbye(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Goodbye news", Body: "About goodbye."})
}

// Holiday sends the holiday message to users.
func Holiday(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Holiday news", Body: "About holiday."})
}

// Security sends the security message to users.
func Security(c notify.Config, users []notify.User) []string {
	return notify.Broadcast(c, users, notify.Message{Subject: "Security news", Body: "About security."})
}
