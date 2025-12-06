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

	// 1) Resample each lap to 'samples' points
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
		lap := NormalizeLapSegment(points[start:end])
		lap = resampleLap(lap, 0, len(lap), samples)
		if lap != nil {
			laps = append(laps, lap)
		}
	}
	if len(laps) == 0 {
		return nil
	}

	// 2) Use first lap as reference
	ref := laps[0]

	aligned := make([][]models.Trackpoint, len(laps))
	aligned[0] = ref

	// Align each subsequent lap to the reference
	for i := 1; i < len(laps); i++ {
		aligned[i] = alignLapToRef(ref, laps[i])
	}

	// 3) Average aligned laps
	master := make([]models.Trackpoint, samples)
	for i := 0; i < samples; i++ {
		var sx, sy float64
		for l := 0; l < len(aligned); l++ {
			sx += aligned[l][i].X
			sy += aligned[l][i].Y
		}
		n := float64(len(aligned))
		master[i] = models.Trackpoint{
			S:     ref[i].S, // distance along lap (local)
			X:     sx / n,
			Y:     sy / n,
			Theta: 0, // optional; you can average heading if needed
		}
	}

	return master
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

// alignLapToRef: rotate + translate 'lap' so it best fits 'ref'.
func alignLapToRef(ref, lap []models.Trackpoint) []models.Trackpoint {
	n := len(ref)
	if len(lap) != n || n == 0 {
		return lap
	}

	// centroids
	var cxRef, cyRef, cxLap, cyLap float64
	for i := 0; i < n; i++ {
		cxRef += ref[i].X
		cyRef += ref[i].Y
		cxLap += lap[i].X
		cyLap += lap[i].Y
	}
	invN := 1.0 / float64(n)
	cxRef *= invN
	cyRef *= invN
	cxLap *= invN
	cyLap *= invN

	// compute a,b for optimal rotation
	var a, b float64
	for i := 0; i < n; i++ {
		rx := ref[i].X - cxRef
		ry := ref[i].Y - cyRef
		lx := lap[i].X - cxLap
		ly := lap[i].Y - cyLap

		a += lx*rx + ly*ry // dot
		b += lx*ry - ly*rx // cross (z component)
	}

	denom := math.Hypot(a, b)
	cosT, sinT := 1.0, 0.0
	if denom > 0 {
		cosT = a / denom
		sinT = b / denom
	}

	// Compute optimal isotropic scale to minimize SSE after rotation.
	var num, den float64
	for i := 0; i < n; i++ {
		lx := lap[i].X - cxLap
		ly := lap[i].Y - cyLap
		rx := ref[i].X - cxRef
		ry := ref[i].Y - cyRef
		rxPrime := cosT*lx - sinT*ly
		ryPrime := sinT*lx + cosT*ly
		num += rx*rxPrime + ry*ryPrime
		den += lx*lx + ly*ly
	}
	scale := 1.0
	if den > 0 {
		scale = num / den
	}

	out := make([]models.Trackpoint, n)
	for i := 0; i < n; i++ {
		dx := lap[i].X - cxLap
		dy := lap[i].Y - cyLap

		x := (cosT*dx - sinT*dy) * scale
		y := (sinT*dx + cosT*dy) * scale
		x += cxRef
		y += cyRef

		out[i] = models.Trackpoint{
			S:     lap[i].S, // local S stays as-is
			X:     x,
			Y:     y,
			Theta: lap[i].Theta, // you can adjust if you like
		}
	}

	// Anchor start point to reference start to reduce rotational/translation smear.
	shiftX := ref[0].X - out[0].X
	shiftY := ref[0].Y - out[0].Y
	if shiftX != 0 || shiftY != 0 {
		for i := 0; i < n; i++ {
			out[i].X += shiftX
			out[i].Y += shiftY
		}
	}

	return out
}

func BuildTrack(samples []models.Sample) ([]models.Trackpoint, error) {
	if len(samples) < 2 {
		return nil, errors.New("not enough samples")
	}

	const (
		minDT    = 0.016
		maxDT    = 0.25
		minSpeed = 0.1
	)

	track := make([]models.Trackpoint, len(samples))

	var (
		x, y float64
		dist float64
		// Seed heading from velocity vector if present, else zero.
		theta = math.Atan2(samples[0].VelZ, samples[0].VelX)
	)

	if math.IsNaN(theta) || math.IsInf(theta, 0) {
		theta = 0
	}

	samples[0].SmoothAx = cleanFloat(samples[0].AccelX, 0)
	track[0] = models.Trackpoint{S: 0, X: 0, Y: 0, Theta: theta}

	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		cur := &samples[i] // pointer so we can modify SmoothAx

		dt := cleanFloat(cur.Time-prev.Time, minDT)
		dt = clamp(dt, minDT, maxDT)

		curAccel := cleanFloat(cur.AccelX, 0)
		cur.SmoothAx = prev.SmoothAx*0.85 + curAccel*0.15

		speed := cleanFloat(cur.Speed, prev.Speed)
		if speed < minSpeed {
			speed = minSpeed
		}

		yawRate := 0.0
		if speed > 2.0 {
			yawRate = cur.SmoothAx / speed
		}

		theta += yawRate * dt

		dx := math.Cos(theta) * speed * dt
		dy := math.Sin(theta) * speed * dt

		x += dx
		y += dy

		dist += math.Hypot(dx, dy)

		track[i] = models.Trackpoint{
			S:     dist,
			X:     x,
			Y:     y,
			Theta: theta,
		}
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

func NormalizeTrack(points []models.Trackpoint, targetLength float64) []models.Trackpoint {
	if len(points) < 2 {
		return points
	}

	var total float64
	for i := 1; i < len(points); i++ {
		dx := points[i].X - points[i-1].X
		dy := points[i].Y - points[i-1].Y
		total += math.Hypot(dx, dy)
	}

	if total == 0 {
		return points
	}

	scale := targetLength / total

	var sumX, sumY float64
	for _, p := range points {
		sumX += p.X
		sumY += p.Y
	}

	cx := sumX / float64(len(points))
	cy := sumY / float64(len(points))

	out := make([]models.Trackpoint, len(points))

	for i, p := range points {
		out[i] = models.Trackpoint{
			S:     p.S * scale,
			X:     (p.X - cx) * scale,
			Y:     (p.Y - cy) * scale,
			Theta: p.Theta,
		}
	}

	return rotateToPrincipalAxis(out)
}

func rotateToPrincipalAxis(points []models.Trackpoint) []models.Trackpoint {
	if len(points) < 2 {
		return points
	}

	// Compute centroid
	var sumX, sumY float64
	for _, p := range points {
		sumX += p.X
		sumY += p.Y
	}
	cx := sumX / float64(len(points))
	cy := sumY / float64(len(points))

	// Compute covariance elements
	var covXX, covXY, covYY float64
	for _, p := range points {
		dx := p.X - cx
		dy := p.Y - cy
		covXX += dx * dx
		covXY += dx * dy
		covYY += dy * dy
	}
	n := float64(len(points))
	covXX /= n
	covXY /= n
	covYY /= n

	// Compute orientation of principal axis
	angle := 0.5 * math.Atan2(2*covXY, covXX-covYY)
	cosA := math.Cos(-angle)
	sinA := math.Sin(-angle)

	// Rotate all points
	out := make([]models.Trackpoint, len(points))
	for i, p := range points {
		dx := p.X - cx
		dy := p.Y - cy

		x := dx*cosA - dy*sinA
		y := dx*sinA + dy*cosA

		out[i] = models.Trackpoint{
			S:     p.S,
			X:     x,
			Y:     y,
			Theta: p.Theta + (-angle),
		}
	}

	return out
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
			S:     p.S,        // keep distance as-is (or recompute if you want)
			X:     p.X - t*dx, // subtract fraction of drift
			Y:     p.Y - t*dy,
			Theta: p.Theta, // heading unchanged
		}
	}

	// Hard snap last point to first to avoid any float leftovers
	out[n-1].X = out[0].X
	out[n-1].Y = out[0].Y

	return out
}

// NormalizeLapSegment closes a lap drift and recomputes arc length so S starts
// at 0 and ends at lap length derived from geometry.
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
