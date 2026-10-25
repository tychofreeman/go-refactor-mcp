package m

type Policy struct{ Max int }

var policy = Policy{Max: 5}

// DefaultPolicy is the policy every desk uses.
func DefaultPolicy() Policy { return Policy{Max: 5} }

func capFee(f int) int {
	policy.Max++
	if f > policy.Max {
		return policy.Max
	}
	return f
}

func Fee(f int) int { return capFee(f) }
