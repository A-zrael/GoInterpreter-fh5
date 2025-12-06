package track

import (
	"forza/models"
	"math"
)

// BuildEvenLapIdx generates lap boundaries assuming 'laps' equally spaced laps by distance.
// Returns start indices for each lap plus a final end-exclusive boundary.
func BuildEvenLapIdx(points []models.Trackpoint, laps int) []int {
	if laps < 1 || len(points) == 0 {
		return []int{0, len(points)}
	}
	totalDist := points[len(points)-1].S
	if totalDist <= 0 {
		return []int{0, len(points)}
	}
	lapLen := totalDist / float64(laps)
	target := lapLen
	out := []int{0}
	for i, p := range points {
		if p.S >= target && len(out) < laps {
			out = append(out, i)
			target += lapLen
		}
	}
	if out[len(out)-1] != len(points) {
		out = append(out, len(points))
	}
	return out
}

// DeriveLapCount returns a reasonable lap count when one is not provided.
func DeriveLapCount(totalDist float64, preferred int) int {
	if preferred > 0 {
		return preferred
	}
	if totalDist <= 0 {
		return 1
	}
	laps := int(math.Round(totalDist / 50000.0))
	if laps < 1 {
		laps = 1
	}
	if laps > 8 {
		laps = 8
	}
	return laps
}

// BuildLapIdx attempts distance-based detection when lapLen is provided, otherwise falls back to even spacing.
func BuildLapIdx(trackPoints []models.Trackpoint, laps int, lapLen float64, lapTol float64, minLapSpacing float64) []int {
	if lapLen > 0 && laps <= 1 {
		idx := FindLapIndicesByDistanceWithMin(trackPoints, lapLen, lapTol, math.Max(minLapSpacing, lapLen*0.2))
		if len(idx) >= 2 {
			return idx
		}
	}
	return BuildEvenLapIdx(trackPoints, laps)
}

// FindLapAndRelS returns the lap number (1-based) and relS within that lap for a point index.
func FindLapAndRelS(lapIdx []int, points []models.Trackpoint, idx int) (int, float64) {
	for lapNum := 1; lapNum < len(lapIdx); lapNum++ {
		start := lapIdx[lapNum-1]
		end := lapIdx[lapNum]
		if idx >= start && idx < end {
			return lapNum, points[idx].S - points[start].S
		}
	}
	return 0, 0
}
