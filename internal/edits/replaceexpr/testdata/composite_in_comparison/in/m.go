package m

type Atlas struct{}

func Rare(k any) bool {
	if k == "atlas" {
		return true
	}
	return false
}
