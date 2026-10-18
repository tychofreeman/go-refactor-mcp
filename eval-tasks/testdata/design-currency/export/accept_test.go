package export

import (
	"encoding/json"
	"testing"

	"example.com/shop/order"
)

var acceptOrders = []order.Order{
	{ID: "A1", Customer: "Ann", CustomerKind: "retail", City: "Springfield", Status: order.Paid, Lines: []order.Line{{SKU: "pen", Qty: 2, UnitCents: 150}}},
	{ID: "B2", Customer: "Bob", CustomerKind: "wholesale", City: "Shelbyville", Status: order.Shipped, Lines: []order.Line{{SKU: "ink", Qty: 12, UnitCents: 500}}},
}

var (
	acceptEUR = Currency{Code: "EUR", Symbol: "€", Decimals: 2}
	acceptJPY = Currency{Code: "JPY", Symbol: "¥", Decimals: 0}
	acceptKWD = Currency{Code: "KWD", Symbol: "KD", Decimals: 3}
)

func TestAcceptCSVCurrency(t *testing.T) {
	for _, tc := range []struct {
		cur  Currency
		want string
	}{
		{acceptEUR, "id,customer,city,total\nA1,Ann,Springfield,3.60\nB2,Bob,Shelbyville,47.04\n"},
		{acceptJPY, "id,customer,city,total\nA1,Ann,Springfield,360\nB2,Bob,Shelbyville,4704\n"},
		{acceptKWD, "id,customer,city,total\nA1,Ann,Springfield,0.360\nB2,Bob,Shelbyville,4.704\n"},
	} {
		if got := CSV(acceptOrders, tc.cur); got != tc.want {
			t.Errorf("%s: got:\n%s\nwant:\n%s", tc.cur.Code, got, tc.want)
		}
	}
}

func TestAcceptJSONCurrency(t *testing.T) {
	var rows []struct{ Total string }
	if err := json.Unmarshal([]byte(JSON(acceptOrders, acceptJPY)), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Total != "360" || rows[1].Total != "4704" {
		t.Errorf("JPY totals %+v, want 360 and 4704", rows)
	}
}

func TestAcceptMarkdownCurrency(t *testing.T) {
	want := "| Order | Customer | Total |\n|---|---|---|\n| A1 | Ann | €3.60 |\n| B2 | Bob | €47.04 |\n\nTotal: €50.64\n"
	if got := Markdown(acceptOrders, acceptEUR); got != want {
		t.Errorf("EUR: got:\n%s\nwant:\n%s", got, want)
	}
	want = "| Order | Customer | Total |\n|---|---|---|\n| A1 | Ann | ¥360 |\n| B2 | Bob | ¥4704 |\n\nTotal: ¥5064\n"
	if got := Markdown(acceptOrders, acceptJPY); got != want {
		t.Errorf("JPY: got:\n%s\nwant:\n%s", got, want)
	}
}
