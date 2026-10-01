package decide

// PrevalenceCorrector implements prior correction (Saerens et al.): a model trained on
// TrainPrevalence (typically oversampled/balanced) is corrected back to NaturalPrevalence
// before it is used for any rupee decision (docs non-negotiable #9: "Prevalence correction
// is explicit and versioned"). Every rupee threshold in the policy depends on this being
// right, so both prevalences are stamped onto the decision, not just the output.
type PrevalenceCorrector struct {
	Version          string
	TrainPrevalence  float64
	NaturalPrevalence float64
}

// Adjust maps a calibrated probability p (calibrated under TrainPrevalence) to the
// probability under NaturalPrevalence.
func (c *PrevalenceCorrector) Adjust(p float64) float64 {
	if c.TrainPrevalence <= 0 || c.TrainPrevalence >= 1 || c.NaturalPrevalence <= 0 || c.NaturalPrevalence >= 1 {
		return p
	}
	ratio := c.NaturalPrevalence / c.TrainPrevalence
	inverseRatio := (1 - c.NaturalPrevalence) / (1 - c.TrainPrevalence)
	num := p * ratio
	den := num + (1-p)*inverseRatio
	if den <= 0 {
		return p
	}
	return num / den
}
