package m

type Atlas struct{}

func Rare(k any) bool {
	if k == (Atlas{}) {
		return true
	}
	return false
}
