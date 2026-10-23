package process

// SummaryTextAccepted decides whether a text of sourceBytes bytes, after
// trimming, gets a paid model request. It is a plain function of a fact every
// run records first (the source-bytes Attribute), so the same function can be
// run over a goal review's export of the past Live runs to see what a changed
// rule would have decided.
func SummaryTextAccepted(sourceBytes int) bool {
	return sourceBytes > 0 && sourceBytes <= MaximumTextSummaryBytes
}
