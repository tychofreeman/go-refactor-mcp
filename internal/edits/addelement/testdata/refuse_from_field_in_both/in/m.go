package m

type bed struct {
	sun  int
	soil string
}

var beds = map[string]bed{
	"north": {sun: 6, soil: "sand"},
	"west":  plain,
}

var plain = bed{sun: 2}

var soils = map[string]string{
	"north": "loam",
	"south": "clay",
}
