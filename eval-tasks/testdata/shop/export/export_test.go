package export

import "testing"

func TestCSV(t *testing.T) {
	want := "id,customer,city,total\nA1,Ann,Springfield,3.60\nB2,Bob,Shelbyville,47.04\nD4,Di,Ogdenville,12.00\n"
	if got := CSV(sample); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSON(t *testing.T) {
	want := `[
  {
    "id": "A1",
    "customer": "Ann",
    "city": "Springfield",
    "total": "3.60"
  },
  {
    "id": "B2",
    "customer": "Bob",
    "city": "Shelbyville",
    "total": "47.04"
  },
  {
    "id": "D4",
    "customer": "Di",
    "city": "Ogdenville",
    "total": "12.00"
  }
]
`
	if got := JSON(sample); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdown(t *testing.T) {
	want := "| Order | Customer | Total |\n|---|---|---|\n| A1 | Ann | $3.60 |\n| B2 | Bob | $47.04 |\n| D4 | Di | $12.00 |\n\nTotal: $62.64\n"
	if got := Markdown(sample); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
