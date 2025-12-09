package track

import "forza/models"

// SmoothSeries applies a simple moving average over the input.
func SmoothSeries(vals []float64, window int) []float64 {
	if window <= 1 {
		return vals
	}
	out := make([]float64, len(vals))
	var sum float64
	for i, v := range vals {
		sum += v
		if i >= window {
			sum -= vals[i-window]
		}
		count := window
		if i+1 < window {
			count = i + 1
		}
		out[i] = sum / float64(count)
	}
	return out
}

// LapIdxFromTelemetry builds lap boundaries whenever LapNumber increases by at least 1.
func LapIdxFromTelemetry(samples []models.Sample) []int {
	if len(samples) == 0 {
		return nil
	}
	idx := []int{0}
	last := samples[0].LapNumber
	for i := 1; i < len(samples); i++ {
		cur := samples[i].LapNumber
		if cur >= last+1 {
			idx = append(idx, i)
			last = cur
		}
	}
	if len(idx) < 2 {
		return nil
	}
	if idx[len(idx)-1] != len(samples) {
		idx = append(idx, len(samples))
	}
	return idx
}
