package track

import "forza/models"

// MapToMaster emits per-point mapping from a lap segment to the master lap.
// scaleS lets the caller stretch/shrink lap S to match master length.
// The provided callback receives (point index within the full session, lap-local
// relS (scaled), point coords, master index, master relS, master coords, distance).
func MapToMaster(lap []models.Trackpoint, master []models.Trackpoint, startIndex int, scaleS float64, emit func(idx int, relS, x, y float64, mi int, mRelS, mx, my, dist float64)) {
	if len(lap) == 0 || len(master) == 0 || emit == nil {
		return
	}
	if scaleS == 0 {
		scaleS = 1
	}

	j := 0
	for i := 0; i < len(lap); i++ {
		relS := (lap[i].S - lap[0].S) * scaleS
		for j+1 < len(master) && master[j+1].S <= relS {
			j++
		}
		closest := j
		if j+1 < len(master) {
			// pick closer between j and j+1 by S
			if relS-master[j].S > master[j+1].S-relS {
				closest = j + 1
			}
		}
		m := master[closest]
		dx := lap[i].X - m.X
		dy := lap[i].Y - m.Y
		distSq := dx*dx + dy*dy
		emit(startIndex+i, relS, lap[i].X, lap[i].Y, closest, m.S, m.X, m.Y, distSq)
	}
}

// MapRelSToMaster maps a single relS/point to the closest master point by S.
func MapRelSToMaster(master []models.Trackpoint, relS float64, px, py float64) (int, float64, float64, float64, float64) {
	if len(master) == 0 {
		return 0, 0, 0, 0, 0
	}
	j := 0
	for j+1 < len(master) && master[j+1].S <= relS {
		j++
	}
	closest := j
	if j+1 < len(master) {
		if relS-master[j].S > master[j+1].S-relS {
			closest = j + 1
		}
	}
	m := master[closest]
	dx := px - m.X
	dy := py - m.Y
	return closest, m.S, m.X, m.Y, dx*dx + dy*dy
}
