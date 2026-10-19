package export

import (
	"testing"

	"example.com/shop/order"
)

var accept2Orders = []order.Order{
	{ID: "A1", Customer: "Ann", CustomerKind: "retail", City: "Springfield", Status: order.Paid, Lines: []order.Line{{SKU: "pen", Qty: 2, UnitCents: 150}}},
	{ID: "R1", Customer: "Ann", CustomerKind: "retail", City: "Springfield", Status: order.Shipped, Lines: []order.Line{{SKU: "refund", Qty: 1, UnitCents: -300}}},
}

func TestAccept2NegativeAmounts(t *testing.T) {
	eur := Currency{Code: "EUR", Symbol: "€", Decimals: 2}
	kwd := Currency{Code: "KWD", Symbol: "KD", Decimals: 3}
	jpy := Currency{Code: "JPY", Symbol: "¥", Decimals: 0}
	if got, want := CSV(accept2Orders, eur), "id,customer,city,total\nA1,Ann,Springfield,3.60\nR1,Ann,Springfield,-3.60\n"; got != want {
		t.Errorf("CSV EUR got:\n%s\nwant:\n%s", got, want)
	}
	if got, want := CSV(accept2Orders, kwd), "id,customer,city,total\nA1,Ann,Springfield,0.360\nR1,Ann,Springfield,-0.360\n"; got != want {
		t.Errorf("CSV KWD got:\n%s\nwant:\n%s", got, want)
	}
	if got, want := CSV(accept2Orders, jpy), "id,customer,city,total\nA1,Ann,Springfield,360\nR1,Ann,Springfield,-360\n"; got != want {
		t.Errorf("CSV JPY got:\n%s\nwant:\n%s", got, want)
	}
	if got, want := Markdown(accept2Orders, eur), "| Order | Customer | Total |\n|---|---|---|\n| A1 | Ann | €3.60 |\n| R1 | Ann | -€3.60 |\n\nTotal: €0.00\n"; got != want {
		t.Errorf("Markdown EUR got:\n%s\nwant:\n%s", got, want)
	}
}
