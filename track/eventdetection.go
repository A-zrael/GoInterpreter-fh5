package track

import (
	"fmt"
	"forza/models"
	"math"
)

type EventThresholds struct {
	StopSpeed          float64 // speed considered stopped
	CrashDecel         float64 // m/s^2 decel to call a crash
	CrashMinPreSpeed   float64 // min speed before crash drop
	CollisionAccelMag  float64 // accel magnitude spike for collision
	CollisionSpeedDrop float64 // required speed drop for collision
	ResetMinDuration   float64 // seconds near-zero to call a reset
	ResetVelEpsilon    float64 // m/s velocity magnitude considered zero
	DedupeWindow       float64 // seconds to dedupe same-type events
	RumbleThreshold    float64 // wheel_on_rumble sum to flag rumble
	PuddleThreshold    float64 // wheel_in_puddle sum to flag puddle
	DriftSlipAngle     float64 // avg abs slip angle (rad) to flag drift
	DriftMinSpeed      float64 // min speed to consider drift (m/s)
	TractionSlip       float64 // combined slip to flag traction loss
	TractionThrottle   float64 // throttle raw min to consider traction loss
}

func defaultEventThresholds() EventThresholds {
	return EventThresholds{
		StopSpeed:          1.0,
		CrashDecel:         -8.0,
		CrashMinPreSpeed:   5.0,
		CollisionAccelMag:  12.0,
		CollisionSpeedDrop: 2.0,
		ResetMinDuration:   1.5,
		ResetVelEpsilon:    0.25,
		DedupeWindow:       1.0,
		RumbleThreshold:    0.8,
		PuddleThreshold:    0.5,
		DriftSlipAngle:     0.3, // ~17 degrees
		DriftMinSpeed:      8.0, // m/s
		TractionSlip:       0.4,
		TractionThrottle:   120,
	}
}

// DetectEvents flags basic driving anomalies (reset, crash, collision).
func DetectEvents(samples []models.Sample) []models.Event {
	th := defaultEventThresholds()
	events := []models.Event{}
	if len(samples) < 2 {
		return events
	}

	lastOfType := make(map[string]float64)

	resetStart := -1
	resetAccum := 0.0
	seenOn := false
	driftActive := false
	tractionActive := false

	lastPos := -1

	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		cur := samples[i]

		// Wait until race actually starts; ignore pre-race zeros.
		if cur.IsRaceOn == 0 && !seenOn {
			continue
		}
		if cur.IsRaceOn != 0 {
			seenOn = true
		} else if seenOn {
			// Once we've seen on-state, stop when it goes back to zero (post-race).
			break
		}
		// If previous sample was off, skip this transition frame.
		if prev.IsRaceOn == 0 {
			continue
		}

		if prev.RacePosition > 0 {
			lastPos = prev.RacePosition
		}

		dt := cur.Time - prev.Time
		if dt <= 0 || dt > 1.0 || math.IsNaN(dt) || math.IsInf(dt, 0) {
			dt = 0
		}

		speedPrev := speedMPS(prev)
		speedCur := speedMPS(cur)
		dSpeed := speedCur - speedPrev
		decel := 0.0
		if dt > 0 {
			decel = dSpeed / dt
		}

		accelMag := math.Sqrt(cur.AccelX*cur.AccelX + cur.AccelY*cur.AccelY + cur.AccelZ*cur.AccelZ)
		velMag := math.Hypot(cur.VelX, cur.VelZ)

		// Position gain/loss and pole changes.
		if lastPos > 0 && cur.RacePosition > 0 && cur.RacePosition != lastPos {
			posChange := cur.RacePosition - lastPos
			var evType string
			note := fmt.Sprintf("position %d → %d", lastPos, cur.RacePosition)
			if posChange < 0 {
				evType = "position_gain"
			} else {
				evType = "position_loss"
			}
			if okToEmit(lastOfType[evType], cur.Time, th.DedupeWindow) {
				events = append(events, models.Event{Index: i, Time: cur.Time, Type: evType, Note: note})
				lastOfType[evType] = cur.Time
			}
			if lastPos == 1 && cur.RacePosition > 1 {
				if okToEmit(lastOfType["pole_loss"], cur.Time, th.DedupeWindow) {
					events = append(events, models.Event{Index: i, Time: cur.Time, Type: "pole_loss", Note: note})
					lastOfType["pole_loss"] = cur.Time
				}
			} else if lastPos > 1 && cur.RacePosition == 1 {
				if okToEmit(lastOfType["pole_gain"], cur.Time, th.DedupeWindow) {
					events = append(events, models.Event{Index: i, Time: cur.Time, Type: "pole_gain", Note: note})
					lastOfType["pole_gain"] = cur.Time
				}
			}
			lastPos = cur.RacePosition
		}

		// Reset detection: sustained near-zero movement.
		if velMag < th.ResetVelEpsilon && speedCur < th.StopSpeed {
			if resetStart == -1 {
				resetStart = i
				resetAccum = 0
			}
			resetAccum += dt
			if resetAccum >= th.ResetMinDuration {
				if okToEmit(lastOfType["reset"], cur.Time, th.DedupeWindow) {
					events = append(events, models.Event{Index: resetStart, Time: samples[resetStart].Time, Type: "reset", Note: "near-zero movement"})
					lastOfType["reset"] = cur.Time
				}
				resetStart = -1
				resetAccum = 0
			}
		} else {
			resetStart = -1
			resetAccum = 0
		}

		// Crash: large decel to near stop.
		if speedPrev > th.CrashMinPreSpeed && speedCur < th.StopSpeed && decel <= th.CrashDecel {
			if okToEmit(lastOfType["crash"], cur.Time, th.DedupeWindow) {
				events = append(events, models.Event{Index: i, Time: cur.Time, Type: "crash", Note: "hard stop"})
				lastOfType["crash"] = cur.Time
			}
		}

		// Collision: accel spike + speed drop but not full stop.
		if accelMag >= th.CollisionAccelMag && dSpeed < -th.CollisionSpeedDrop && speedCur >= th.StopSpeed {
			if okToEmit(lastOfType["collision"], cur.Time, th.DedupeWindow) {
				events = append(events, models.Event{Index: i, Time: cur.Time, Type: "collision", Note: "accel spike + speed drop"})
				lastOfType["collision"] = cur.Time
			}
		}

		// Rumble strip contact.
		rumble := cur.WheelOnRumbleFL + cur.WheelOnRumbleFR + cur.WheelOnRumbleRL + cur.WheelOnRumbleRR
		if rumble >= th.RumbleThreshold && okToEmit(lastOfType["rumble"], cur.Time, th.DedupeWindow) {
			events = append(events, models.Event{Index: i, Time: cur.Time, Type: "rumble", Note: "wheel on rumble"})
			lastOfType["rumble"] = cur.Time
		}

		// Puddle/wet contact.
		puddle := cur.WheelInPuddleFL + cur.WheelInPuddleFR + cur.WheelInPuddleRL + cur.WheelInPuddleRR
		if puddle >= th.PuddleThreshold && okToEmit(lastOfType["puddle"], cur.Time, th.DedupeWindow) {
			events = append(events, models.Event{Index: i, Time: cur.Time, Type: "puddle", Note: "wheel in puddle"})
			lastOfType["puddle"] = cur.Time
		}

		// Drift: sustained high slip angles at speed.
		slipAng := (math.Abs(cur.TireSlipAngleFL) + math.Abs(cur.TireSlipAngleFR) + math.Abs(cur.TireSlipAngleRL) + math.Abs(cur.TireSlipAngleRR)) / 4
		if slipAng >= th.DriftSlipAngle && speedCur >= th.DriftMinSpeed {
			if !driftActive && okToEmit(lastOfType["drift"], cur.Time, th.DedupeWindow) {
				events = append(events, models.Event{Index: i, Time: cur.Time, Type: "drift", Note: "high slip angle"})
				lastOfType["drift"] = cur.Time
			}
			driftActive = true
		} else {
			driftActive = false
		}

		// Traction loss: high combined slip under throttle.
		avgSlip := (cur.TireCombinedSlipFL + cur.TireCombinedSlipFR + cur.TireCombinedSlipRL + cur.TireCombinedSlipRR) / 4
		if avgSlip >= th.TractionSlip && cur.ThrottleRaw >= int(th.TractionThrottle) && speedCur >= th.StopSpeed {
			if !tractionActive && okToEmit(lastOfType["traction"], cur.Time, th.DedupeWindow) {
				events = append(events, models.Event{Index: i, Time: cur.Time, Type: "traction", Note: "traction loss"})
				lastOfType["traction"] = cur.Time
			}
			tractionActive = true
		} else {
			tractionActive = false
		}
	}

	return events
}

func okToEmit(lastTime float64, now float64, window float64) bool {
	if lastTime == 0 {
		return true
	}
	return now-lastTime >= window
}

func speedMPS(s models.Sample) float64 {
	if s.Speed > 0 {
		return s.Speed
	}
	if s.SpeedMPS > 0 {
		return s.SpeedMPS
	}
	return 0
}

// MappedPoint represents a point mapped to master coordinates for cross-car comparisons.
type MappedPoint struct {
	Time    float64
	Lap     int
	RelS    float64
	MasterX float64
	MasterY float64
}

type OvertakeEvent struct {
	Source  string
	Target  string
	Time    float64
	Lap     int
	RelS    float64
	MasterX float64
	MasterY float64
}

// DetectOvertakes finds overtake events between cars using their mapped points.
// Input map keys are source names; values are ordered slices of points.
func DetectOvertakes(mapped map[string][]MappedPoint) []OvertakeEvent {
	var events []OvertakeEvent
	for aName, aPts := range mapped {
		for bName, bPts := range mapped {
			if aName >= bName {
				continue
			}
			evs := detectPair(aName, bName, aPts, bPts)
			events = append(events, evs...)
		}
	}
	return events
}

func detectPair(aName, bName string, aPts, bPts []MappedPoint) []OvertakeEvent {
	var out []OvertakeEvent
	if len(aPts) == 0 || len(bPts) == 0 {
		return out
	}
	lapA := lapLengthsFromMapped(aPts)
	lapB := lapLengthsFromMapped(bPts)
	maxT := aPts[len(aPts)-1].Time
	if bPts[len(bPts)-1].Time < maxT {
		maxT = bPts[len(bPts)-1].Time
	}

	ia, ib := 0, 0
	prevAhead := 0
	for ia < len(aPts) && ib < len(bPts) {
		t := aPts[ia].Time
		if bPts[ib].Time < t {
			t = bPts[ib].Time
		}
		if t > maxT {
			break
		}
		pa, oka := pointAtTimeMapped(aPts, t)
		pb, okb := pointAtTimeMapped(bPts, t)
		if !oka || !okb {
			break
		}
		progA := progressMapped(pa, lapA)
		progB := progressMapped(pb, lapB)
		ahead := 0
		if progA > progB {
			ahead = 1
		} else if progB > progA {
			ahead = -1
		}
		if prevAhead != 0 && ahead != 0 && ahead != prevAhead {
			if ahead > 0 {
				out = append(out, OvertakeEvent{
					Source:  aName,
					Target:  bName,
					Time:    t,
					Lap:     pa.Lap,
					RelS:    pa.RelS,
					MasterX: pa.MasterX,
					MasterY: pa.MasterY,
				})
			} else {
				out = append(out, OvertakeEvent{
					Source:  bName,
					Target:  aName,
					Time:    t,
					Lap:     pb.Lap,
					RelS:    pb.RelS,
					MasterX: pb.MasterX,
					MasterY: pb.MasterY,
				})
			}
		}
		prevAhead = ahead
		if ia+1 < len(aPts) && (ib+1 >= len(bPts) || aPts[ia+1].Time <= bPts[ib+1].Time) {
			ia++
		} else {
			ib++
		}
	}
	return out
}

func pointAtTimeMapped(points []MappedPoint, t float64) (MappedPoint, bool) {
	if len(points) == 0 {
		return MappedPoint{}, false
	}
	if t <= points[0].Time {
		return points[0], true
	}
	if t >= points[len(points)-1].Time {
		return points[len(points)-1], true
	}
	lo, hi := 0, len(points)-1
	for hi-lo > 1 {
		mid := (hi + lo) >> 1
		if points[mid].Time <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	p1, p2 := points[lo], points[hi]
	span := p2.Time - p1.Time
	if span <= 0 {
		return p1, true
	}
	alpha := (t - p1.Time) / span
	return MappedPoint{
		Time:    t,
		Lap:     p1.Lap,
		RelS:    p1.RelS + (p2.RelS-p1.RelS)*alpha,
		MasterX: p1.MasterX + (p2.MasterX-p1.MasterX)*alpha,
		MasterY: p1.MasterY + (p2.MasterY-p1.MasterY)*alpha,
	}, true
}

func lapLengthsFromMapped(points []MappedPoint) map[int]float64 {
	m := make(map[int]float64)
	for _, p := range points {
		if p.RelS > m[p.Lap] {
			m[p.Lap] = p.RelS
		}
	}
	return m
}

func progressMapped(p MappedPoint, lapLen map[int]float64) float64 {
	lenLap := lapLen[p.Lap]
	if lenLap <= 0 {
		lenLap = 1
	}
	return float64(p.Lap-1) + p.RelS/lenLap
}
