package track

import (
	"forza/models"
	"math"
)

// LapMetrics holds timing for a lap and its sectors.
type LapMetrics struct {
	Lap         int       `json:"lap"`
	LapTime     float64   `json:"lapTime"`
	SectorTime  []float64 `json:"sectorTime,omitempty"`
	SectorDelta []float64 `json:"sectorDelta,omitempty"`
}

// ComputeLapMetrics calculates lap and sector times for a session.
// sectors splits a lap into N equal distance sectors; if sectors<=0, only lap times are returned.
func ComputeLapMetrics(samples []models.Sample, points []models.Trackpoint, lapIdx []int, sectors int) []LapMetrics {
	if len(samples) == 0 || len(points) == 0 || len(lapIdx) < 2 {
		return nil
	}
	if sectors < 0 {
		sectors = 0
	}

	var out []LapMetrics

	for lapNum := 1; lapNum < len(lapIdx); lapNum++ {
		start := lapIdx[lapNum-1]
		end := lapIdx[lapNum]
		if start < 0 || end > len(samples) || end > len(points) || end <= start+1 {
			continue
		}

		// Lap time
		lt := samples[end-1].Time - samples[start].Time
		lm := LapMetrics{Lap: lapNum, LapTime: lt}

		// Sector times (distance-based, equal slices of lap distance)
		if sectors > 0 {
			sectorTimes := make([]float64, sectors)
			s0 := points[start].S
			sLap := points[end-1].S - s0
			if sLap < 0 {
				sLap = 0
			}
			idxPtr := start
			for s := 0; s < sectors; s++ {
				segStart := s0 + (float64(s)*sLap)/float64(sectors)
				segEnd := s0 + (float64(s+1)*sLap)/float64(sectors)
				// find start index for segStart
				for idxPtr < end && points[idxPtr].S < segStart {
					idxPtr++
				}
				segIdxStart := idxPtr
				// advance to segEnd
				for idxPtr < end && points[idxPtr].S < segEnd {
					idxPtr++
				}
				segIdxEnd := idxPtr
				if segIdxEnd >= end {
					segIdxEnd = end - 1
				}
				if segIdxEnd <= segIdxStart {
					sectorTimes[s] = 0
					continue
				}
				sectorTimes[s] = samples[segIdxEnd].Time - samples[segIdxStart].Time
			}
			lm.SectorTime = sectorTimes
		}

		out = append(out, lm)
	}

	if sectors > 0 && len(out) > 0 {
		best := make([]float64, sectors)
		for i := range best {
			best[i] = math.Inf(1)
		}
		for _, lm := range out {
			for i, t := range lm.SectorTime {
				if t > 0 && t < best[i] {
					best[i] = t
				}
			}
		}
		for i := range out {
			if len(out[i].SectorTime) == 0 {
				continue
			}
			out[i].SectorDelta = make([]float64, len(out[i].SectorTime))
			for j, t := range out[i].SectorTime {
				if best[j] == math.Inf(1) || t == 0 {
					out[i].SectorDelta[j] = 0
					continue
				}
				out[i].SectorDelta[j] = t - best[j]
			}
		}
	}

	return out
}
