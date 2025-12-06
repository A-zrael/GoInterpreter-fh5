package track

import (
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

	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		cur := samples[i]

		dt := cur.Time - prev.Time
		if dt <= 0 || dt > 1.0 || math.IsNaN(dt) || math.IsInf(dt, 0) {
			dt = 0
		}

		speedPrev := cleanFloat(prev.Speed, 0)
		speedCur := cleanFloat(cur.Speed, 0)
		dSpeed := speedCur - speedPrev
		decel := 0.0
		if dt > 0 {
			decel = dSpeed / dt
		}

		accelMag := math.Sqrt(cur.AccelX*cur.AccelX + cur.AccelY*cur.AccelY + cur.AccelZ*cur.AccelZ)
		velMag := math.Hypot(cur.VelX, cur.VelZ)

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
	}

	return events
}

func okToEmit(lastTime float64, now float64, window float64) bool {
	if lastTime == 0 {
		return true
	}
	return now-lastTime >= window
}
