package notices

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

func TestNotices(t *testing.T) {
	l := lending.Loan{Item: lending.Item{ID: "i", Title: "Heat", Kind: "dvd"}, Member: lending.Member{Name: "Ann", Phone: "555", City: " Oslo "}, Day: 0}
	if got, want := Reminders([]lending.Loan{l}, 8), []string{"Ann: DVD: Heat is due"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Reminders = %q", got)
	}
	if got := Courtesy([]lending.Loan{l}, 12); got != nil {
		t.Errorf("Courtesy after the grace days = %q", got)
	}
	if !Media(l.Item) || Fragile(l.Item) || Header(l.Member) != "To Ann, Oslo" || UpperStreet(l.Member) != "" {
		t.Errorf("Media, Fragile, Header or UpperStreet")
	}
}
