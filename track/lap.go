package track

import (
	"forza/models"
	"math"
	"sort"
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
// If enforceLapCount is true, detected laps will be capped to the requested count.
func BuildLapIdx(trackPoints []models.Trackpoint, laps int, lapLen float64, lapTol float64, minLapSpacing float64, enforceLapCount bool, radius float64) []int {
	if radius <= 0 {
		radius = 10
	}

	// Prefer positional detection now that absolute coordinates are available.
	posIdx := DetectLapsNearStart(trackPoints, radius, minLapSpacing)
	if len(posIdx) >= 2 {
		// Prune obviously spurious laps that are much shorter than the median.
		if len(posIdx) > 2 {
			lens := make([]float64, 0, len(posIdx)-1)
			for i := 1; i < len(posIdx); i++ {
				start := posIdx[i-1]
				end := posIdx[i]
				if end > len(trackPoints) {
					end = len(trackPoints)
				}
				if end <= start {
					continue
				}
				lens = append(lens, trackPoints[end-1].S-trackPoints[start].S)
			}
			if len(lens) > 0 {
				med := median(lens)
				var filtered []int
				filtered = append(filtered, posIdx[0])
				for i := 1; i < len(posIdx)-1; i++ {
					start := posIdx[i-1]
					end := posIdx[i]
					if end > len(trackPoints) {
						end = len(trackPoints)
					}
					if end <= start {
						continue
					}
					segLen := trackPoints[end-1].S - trackPoints[start].S
					if segLen >= med*0.8 && segLen >= minLapSpacing {
						filtered = append(filtered, posIdx[i])
					}
				}
				filtered = append(filtered, posIdx[len(posIdx)-1])
				posIdx = filtered
			}
		}
		// If caller supplied an expected lap count, trim extras while keeping end boundary.
		if enforceLapCount && laps > 0 && len(posIdx)-1 > laps {
			posIdx = append(posIdx[:laps], len(trackPoints))
		}
		return posIdx
	}

	if lapLen > 0 && laps <= 1 {
		idx := FindLapIndicesByDistanceWithMin(trackPoints, lapLen, lapTol, math.Max(minLapSpacing, lapLen*0.2))
		if len(idx) >= 2 {
			return idx
		}
	}
	return BuildEvenLapIdx(trackPoints, laps)
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	c := append([]float64(nil), vals...)
	sort.Float64s(c)
	mid := len(c) / 2
	if len(c)%2 == 0 {
		return (c[mid-1] + c[mid]) / 2
	}
	return c[mid]
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

// IsLoopTrack returns true when the path comes back near the start (radius, min spacing).
func IsLoopTrack(points []models.Trackpoint, radius float64, minLapDistance float64) bool {
	if len(points) == 0 {
		return false
	}
	if radius <= 0 {
		radius = 10
	}
	idx := DetectLapsNearStart(points, radius, minLapDistance)
	return len(idx) > 2
}

// IsLoopByProximity returns true if the path ends within radius of the start.
func IsLoopByProximity(points []models.Trackpoint, radius float64) bool {
	if len(points) < 2 {
		return false
	}
	start := points[0]
	end := points[len(points)-1]
	dx := end.X - start.X
	dy := end.Y - start.Y
	return dx*dx+dy*dy <= radius*radius
}
