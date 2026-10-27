package match

func (d Decision) String() string {
	switch d {
	case DecisionPlay:
		return "play"
	case DecisionAsk:
		return "ask"
	default:
		return "not_found"
	}
}

// Best returns the top candidate, if any.
func (r Result) Best() (Candidate, bool) {
	if len(r.Candidates) == 0 {
		return Candidate{}, false
	}

	return r.Candidates[0], true
}
