package track

import (
	"errors"
	"forza/models"
	"math"
)

// --- MASTER LAP BUILDING ---

// BuildMasterLap v2: resample each lap, align to lap 1, then average.
// lapIdx should contain start indices for each lap, with a final boundary
// equal to len(points) (end-exclusive).
func BuildMasterLap(points []models.Trackpoint, lapIdx []int, samples int) []models.Trackpoint {
	if len(lapIdx) < 2 || samples < 2 {
		return nil
	}

	// Make sure final boundary reaches end of slice (end-exclusive).
	if lapIdx[len(lapIdx)-1] != len(points) {
		lapIdx = append(lapIdx, len(points))
	}

	// 1) Resample each lap to 'samples' points using absolute positions (no rotation/closing).
	laps := make([][]models.Trackpoint, 0, len(lapIdx)-1)
	for i := 0; i < len(lapIdx)-1; i++ {
		start := lapIdx[i]
		end := lapIdx[i+1]
		if end > len(points) {
			end = len(points)
		}
		if end <= start+1 {
			continue
		}
		lap := resampleLap(NormalizeLapSegment(points[start:end]), 0, end-start, samples)
		if lap != nil {
			laps = append(laps, lap)
		}
	}
	if len(laps) == 0 {
		return nil
	}

	// 2) Average laps in world space (no rotation/scale).
	master := make([]models.Trackpoint, samples)
	for i := 0; i < samples; i++ {
		var sx, sy, st float64
		for l := 0; l < len(laps); l++ {
			sx += laps[l][i].X
			sy += laps[l][i].Y
			st += laps[l][i].Theta
		}
		n := float64(len(laps))
		master[i] = models.Trackpoint{
			S:     laps[0][i].S, // local distance along lap
			X:     sx / n,
			Y:     sy / n,
			Theta: st / n,
		}
	}

	return master
}

// BuildMasterPath builds a master path for an open/sprint-style run.
// It re-bases arc length to start at 0 without forcing the path to close,
// then resamples to the requested number of samples.
func BuildMasterPath(points []models.Trackpoint, samples int, closeLoop bool) []models.Trackpoint {
	if len(points) < 2 || samples < 2 {
		return nil
	}
	// Preserve absolute positions; only recompute arc length for spacing.
	prep := RecomputeArcLength(points)
	return resampleLap(prep, 0, len(prep), samples)
}

// resampleLap: resample lap [start:end] to 'samples' pts, evenly spaced by S.
func resampleLap(points []models.Trackpoint, start, end, samples int) []models.Trackpoint {
	if end <= start+1 || samples < 2 {
		return nil
	}

	lap := points[start:end]
	s0 := lap[0].S
	sEnd := lap[len(lap)-1].S
	lapLen := sEnd - s0
	if lapLen <= 0 {
		return nil
	}

	out := make([]models.Trackpoint, samples)

	j := 0
	for i := 0; i < samples; i++ {
		targetLocal := float64(i) * lapLen / float64(samples-1)
		targetS := s0 + targetLocal

		for j < len(lap)-1 && lap[j+1].S < targetS {
			j++
		}

		if j == len(lap)-1 {
			out[i] = lap[len(lap)-1]
			out[i].S = targetLocal
			continue
		}

		p1 := lap[j]
		p2 := lap[j+1]

		denom := p2.S - p1.S
		t := 0.0
		if denom > 0 {
			t = (targetS - p1.S) / denom
		}

		x := p1.X + t*(p2.X-p1.X)
		y := p1.Y + t*(p2.Y-p1.Y)

		out[i] = models.Trackpoint{
			S:     targetLocal, // local S from 0..lapLen
			X:     x,
			Y:     y,
			Theta: p1.Theta + t*(p2.Theta-p1.Theta),
		}
	}

	return out
}

func BuildTrack(samples []models.Sample) ([]models.Trackpoint, error) {
	if len(samples) < 2 {
		return nil, errors.New("not enough samples")
	}

	track := make([]models.Trackpoint, len(samples))

	// Re-base world coordinates so the first sample sits at origin; makes downstream
	// math easier to read and avoids very large coordinates.
	originX := cleanFloat(samples[0].PosX, 0)
	originZ := cleanFloat(samples[0].PosZ, 0)

	prevX := samples[0].PosX - originX
	prevZ := samples[0].PosZ - originZ
	dist := 0.0

	heading := worldHeading(samples[0], 0, 0)
	track[0] = models.Trackpoint{S: 0, X: prevX, Y: prevZ, Theta: heading}

	for i := 1; i < len(samples); i++ {
		cur := samples[i]
		curX := cur.PosX - originX
		curZ := cur.PosZ - originZ

		dx := curX - prevX
		dz := curZ - prevZ
		step := math.Hypot(dx, dz)
		dist += step

		heading = worldHeading(cur, dx, dz)
		if heading == 0 && i > 0 {
			heading = track[i-1].Theta
		}

		track[i] = models.Trackpoint{
			S:     dist,
			X:     curX,
			Y:     curZ,
			Theta: heading,
		}

		prevX = curX
		prevZ = curZ
	}

	return track, nil
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func cleanFloat(v, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	return v
}

// worldHeading chooses the best available heading for a sample. Prefer the game's yaw;
// fall back to velocity vector, then path delta.
func worldHeading(s models.Sample, dx, dz float64) float64 {
	if !math.IsNaN(s.Yaw) && !math.IsInf(s.Yaw, 0) && s.Yaw != 0 {
		return s.Yaw
	}
	if s.VelX != 0 || s.VelZ != 0 {
		return math.Atan2(s.VelZ, s.VelX)
	}
	if dx != 0 || dz != 0 {
		return math.Atan2(dz, dx)
	}
	return 0
}

// DetectLapsNearStart marks lap boundaries each time we come back near the start
// point (within radius) after traveling at least minLapDistance since the
// previous boundary. Indices include 0 as the first lap start.
func DetectLapsNearStart(points []models.Trackpoint, radius float64, minLapDistance float64) []int {
	if len(points) == 0 {
		return nil
	}

	startX := points[0].X
	startY := points[0].Y

	r2 := radius * radius

	indices := []int{0}
	lastS := points[0].S

	for i := 1; i < len(points); i++ {
		dx := points[i].X - startX
		dy := points[i].Y - startY
		if dx*dx+dy*dy <= r2 && points[i].S-lastS >= minLapDistance {
			indices = append(indices, i)
			lastS = points[i].S
		}
	}

	if indices[len(indices)-1] != len(points) {
		indices = append(indices, len(points))
	}

	return indices
}
func FindLapIndicesByDistance(points []models.Trackpoint, expectedLap float64, tolerance float64) []int {
	return FindLapIndicesByDistanceWithMin(points, expectedLap, tolerance, expectedLap*0.5)
}

// FindLapIndicesByDistanceWithMin marks a lap when cumulative S advances by
// ~expectedLap (within tolerance) and by at least minLapDistance since the last
// boundary. Use a generous minLapDistance to suppress false positives on noisy
// integrations.
func FindLapIndicesByDistanceWithMin(points []models.Trackpoint, expectedLap float64, tolerance float64, minLapDistance float64) []int {
	if len(points) < 2 {
		return nil
	}

	result := []int{0}
	lastS := points[0].S

	for i := 1; i < len(points); i++ {
		lapDist := points[i].S - lastS
		if lapDist >= expectedLap-tolerance && lapDist >= minLapDistance {
			result = append(result, i)
			lastS = points[i].S
		}
	}

	if result[len(result)-1] != len(points) {
		result = append(result, len(points))
	}

	return result
}

func CloseLoop(points []models.Trackpoint) []models.Trackpoint {
	n := len(points)
	if n < 2 {
		return points
	}

	start := points[0]
	end := points[n-1]

	dx := end.X - start.X
	dy := end.Y - start.Y

	// If already basically closed, just return
	if dx == 0 && dy == 0 {
		return points
	}

	out := make([]models.Trackpoint, n)
	for i, p := range points {
		t := float64(i) / float64(n-1) // 0 at start, 1 at end
		out[i] = models.Trackpoint{
			S:     p.S,
			X:     p.X - t*dx,
			Y:     p.Y - t*dy,
			Theta: p.Theta,
		}
	}

	// Hard snap last point to first to avoid float leftovers.
	out[n-1].X = out[0].X
	out[n-1].Y = out[0].Y

	return out
}

func NormalizeLapSegment(points []models.Trackpoint) []models.Trackpoint {
	closed := CloseLoop(points)
	return RecomputeArcLength(closed)
}

func RecomputeArcLength(points []models.Trackpoint) []models.Trackpoint {
	if len(points) == 0 {
		return points
	}
	out := make([]models.Trackpoint, len(points))
	out[0] = points[0]
	out[0].S = 0
	for i := 1; i < len(points); i++ {
		dx := points[i].X - points[i-1].X
		dy := points[i].Y - points[i-1].Y
		out[i] = points[i]
		out[i].S = out[i-1].S + math.Hypot(dx, dy)
	}
	return out
}
