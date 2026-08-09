package store

import "sort"

// Percentile returns the p-th percentile (0..1) from values using linear interpolation.
// Values are sorted in place.
func Percentile(values []float64, p float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return values[0]
	}
	sort.Float64s(values)
	if p <= 0 {
		return values[0]
	}
	if p >= 1 {
		return values[n-1]
	}
	rank := p * float64(n-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= n {
		return values[n-1]
	}
	frac := rank - float64(lo)
	return values[lo]*(1-frac) + values[hi]*frac
}
