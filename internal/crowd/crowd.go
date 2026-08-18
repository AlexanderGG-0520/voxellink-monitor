// Package crowd evaluates passive player reports independently from probes.
package crowd

import "math"

// Signal is deliberately conservative: a small Minecraft community should not
// be marked degraded because one player has a local connectivity problem.
type Signal struct {
	Reports, Baseline, Threshold int
	Anomalous                    bool
}

// Evaluate requires three independent reports and a 3x increase above the
// matching time-of-week baseline, with a two-report absolute margin.
func Evaluate(reports int, baseline float64) Signal {
	threshold := int(math.Ceil(baseline*3)) + 2
	if threshold < 3 {
		threshold = 3
	}
	return Signal{Reports: reports, Baseline: int(math.Round(baseline)), Threshold: threshold, Anomalous: reports >= threshold}
}
