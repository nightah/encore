package match

// DefaultConfig returns the tuned defaults.
func DefaultConfig() Config {
	return Config{
		PlayScore:       0.90,
		PlayMarginScore: 0.80,
		PlayMargin:      0.10,
		TieMargin:       0.02,
		AskScore:        0.60,
		CandidatePool:   200,
		Limit:           10,
		MaxBoost:        0.05,
		CoveragePenalty: 0.5,
		CompactCoverage: 0.9,
		DerivedPenalty:  0.95,
		Retriever:       RetrieverBleve,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()

	setF := func(v *float64, def float64) {
		if *v == 0 {
			*v = def
		}
	}

	setF(&c.PlayScore, d.PlayScore)
	setF(&c.PlayMarginScore, d.PlayMarginScore)
	setF(&c.PlayMargin, d.PlayMargin)
	setF(&c.TieMargin, d.TieMargin)
	setF(&c.AskScore, d.AskScore)
	setF(&c.MaxBoost, d.MaxBoost)
	setF(&c.CoveragePenalty, d.CoveragePenalty)
	setF(&c.CompactCoverage, d.CompactCoverage)
	setF(&c.DerivedPenalty, d.DerivedPenalty)

	if c.CandidatePool <= 0 {
		c.CandidatePool = d.CandidatePool
	}

	if c.Limit <= 0 {
		c.Limit = d.Limit
	}

	if c.Retriever == "" {
		c.Retriever = d.Retriever
	}

	return c
}

// decide expects cands sorted. exactDocs counts distinct Docs whose name matched the query exactly.
func (c Config) decide(cands []Candidate, exactDocs int) (Decision, float64) {
	if len(cands) == 0 {
		return DecisionNotFound, 0
	}

	top := cands[0].Score

	margin := top
	if len(cands) > 1 {
		margin = top - cands[1].Score
	}

	switch {
	case exactDocs > 1:
		return DecisionAsk, margin
	case exactDocs == 1, top >= c.PlayScore && margin >= c.TieMargin, top >= c.PlayMarginScore && margin >= c.PlayMargin:
		return DecisionPlay, margin
	case top >= c.AskScore:
		return DecisionAsk, margin
	default:
		return DecisionNotFound, margin
	}
}
