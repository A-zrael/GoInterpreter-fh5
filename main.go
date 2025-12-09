package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"forza/models"
	"forza/track"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type masterOut struct {
	RelS    float64 `json:"relS"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Surface string  `json:"surface,omitempty"`
}

type heatOut struct {
	Index    int     `json:"index"`
	RelS     float64 `json:"relS"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	AvgAccel float64 `json:"avgAccel"`
	Surface  string  `json:"surface,omitempty"`
}

type eventOut struct {
	Type       string  `json:"type"`
	Source     string  `json:"source"`
	Target     string  `json:"target,omitempty"`
	Index      int     `json:"index"`
	Time       float64 `json:"time"`
	Note       string  `json:"note"`
	Lap        int     `json:"lap,omitempty"`
	RelS       float64 `json:"relS,omitempty"`
	MasterIdx  int     `json:"masterIdx,omitempty"`
	MasterRelS float64 `json:"masterRelS,omitempty"`
	MasterX    float64 `json:"masterX,omitempty"`
	MasterY    float64 `json:"masterY,omitempty"`
	DistanceSq float64 `json:"distanceSq,omitempty"`
}

type carPoint struct {
	Time     float64 `json:"time"`
	Lap      int     `json:"lap"`
	RelS     float64 `json:"relS"`
	Heading  float64 `json:"heading"`
	MasterX  float64 `json:"masterX"`
	MasterY  float64 `json:"masterY"`
	SpeedMPH float64 `json:"speedMPH"`
	SpeedKMH float64 `json:"speedKMH"`
	Gear     int     `json:"gear"`
	Delta    float64 `json:"delta"`
	LongAcc  float64 `json:"longAcc"`
	LatAcc   float64 `json:"latAcc"`
	YawRate  float64 `json:"yawRate"`
	YawDegS  float64 `json:"yawDegS"`
	Throttle float64 `json:"throttle"`
	Brake    float64 `json:"brake"`
	SteerDeg float64 `json:"steerDeg"`
	// Optional direct driver inputs from telemetry (normalized 0-1 or -1..1).
	ThrottleInput float64 `json:"throttleInput,omitempty"`
	BrakeInput    float64 `json:"brakeInput,omitempty"`
	SteerInput    float64 `json:"steerInput,omitempty"`
	SuspFL        float64 `json:"suspFL,omitempty"`
	SuspFR        float64 `json:"suspFR,omitempty"`
	SuspRL        float64 `json:"suspRL,omitempty"`
	SuspRR        float64 `json:"suspRR,omitempty"`
	TireTempFL    float64 `json:"tireTempFL,omitempty"`
	TireTempFR    float64 `json:"tireTempFR,omitempty"`
	TireTempRL    float64 `json:"tireTempRL,omitempty"`
	TireTempRR    float64 `json:"tireTempRR,omitempty"`
}

type carOut struct {
	Source   string             `json:"source"`
	Points   []carPoint         `json:"points,omitempty"`
	LapTimes []track.LapMetrics `json:"lapTimes,omitempty"`
	RaceType string             `json:"raceType,omitempty"`
}

func main() {
	var filePaths multiFlag
	flag.Var(&filePaths, "file", "Path to telemetry CSV file (repeatable)")
	var folderPaths multiFlag
	flag.Var(&folderPaths, "folder", "Folder containing telemetry CSV files (repeatable, recursive)")
	lapLen := flag.Float64("lap-length", 0, "Expected lap length in meters (0 to autodetect by start crossing)")
	lapTol := flag.Float64("lap-tol", 25, "Tolerance for lap length matching (meters)")
	startFinishRadius := flag.Float64("start-finish-radius", 10, "Radius (m) to consider crossing start/finish for lap detection")
	lapCount := flag.Int("lap-count", 0, "Known lap count; with lap-length=0, lap length is estimated as total distance / lap-count")
	minLapSpacing := flag.Float64("min-lap-spacing", 200, "Minimum distance (m) between lap boundaries when using distance-based detection")
	masterSamples := flag.Int("master-samples", 4000, "Resampled points per lap when building master lap")
	useMaster := flag.Bool("use-master", true, "If true, output the averaged master lap instead of per-lap raw points")
	outPath := flag.String("out", "", "Write JSON to file instead of stdout; implied web/data.json when -serve is set")
	sprintMode := flag.Bool("sprint", false, "Treat input as sprint (no lap crossing); if false, assume lapped race")
	serve := flag.Bool("serve", true, "Generate JSON then serve the viewer locally")
	addr := flag.String("addr", ":8080", "Listen address when -serve is enabled")
	flag.Parse()

	// Collect input files from flags
	inputFiles := append([]string{}, filePaths...)
	if len(folderPaths) > 0 {
		inputFiles = append(inputFiles, filesFromFolders(folderPaths)...)
	}
	if len(inputFiles) == 0 {
		fmt.Fprintf(os.Stderr, "no CSV files found; provide -file and/or -folder\n")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "input files: %d\n", len(inputFiles))

	var (
		allPoints      []models.Trackpoint
		allLapIdx      []int
		lapsAdded      int
		sessionLogs    []string
		masterTrack    []models.Trackpoint
		sessions       []sessionResult
		detectedSprint bool
	)
	allLapIdx = append(allLapIdx, 0)

	results := make(chan sessionResult, len(inputFiles))
	var wg sync.WaitGroup
	for _, path := range inputFiles {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			samples, err := LoadSamplesFromCSV(p)
			if err != nil {
				results <- sessionResult{path: p, err: fmt.Errorf("load: %w", err)}
				return
			}
			telemetryLapIdx := track.LapIdxFromTelemetry(samples)
			tp, err := track.BuildTrack(samples)
			if err != nil {
				results <- sessionResult{path: p, err: fmt.Errorf("track: %w", err)}
				return
			}
			events := track.DetectEvents(samples)
			sessionDist := tp[len(tp)-1].S
			sessionTime := samples[len(samples)-1].Time - samples[0].Time
			lapIdx := track.DetectLapsNearStart(tp, *startFinishRadius, *minLapSpacing)
			loop := len(lapIdx) > 2
			raceType := "sprint"
			laps := 1
			if *sprintMode {
				raceType = "sprint"
			} else if telemetryLapIdx != nil {
				raceType = "lapped"
				laps = len(telemetryLapIdx) - 1
			} else if *lapCount > 0 {
				laps = *lapCount
				if laps > 1 {
					raceType = "lapped"
				}
			} else if loop {
				raceType = "lapped"
				laps = track.DeriveLapCount(sessionDist, *lapCount)
			}
			if raceType == "lapped" {
				if telemetryLapIdx != nil {
					lapIdx = telemetryLapIdx
				} else {
					enforce := *lapCount > 0
					lapIdx = track.BuildLapIdx(tp, laps, *lapLen, *lapTol, *minLapSpacing, enforce, *startFinishRadius)
					if enforce && len(lapIdx) == 2 && laps > 1 {
						lapIdx = track.BuildEvenLapIdx(tp, laps)
					}
				}
			} else {
				lapIdx = []int{0, len(tp)}
			}
			results <- sessionResult{
				path:    p,
				track:   tp,
				samples: samples,
				events:  events,
				race:    raceType,
				lapIdx:  lapIdx,
				dist:    sessionDist,
				dur:     sessionTime,
			}
		}(path)
	}
	wg.Wait()
	close(results)

	for res := range results {
		if res.err != nil {
			fmt.Fprintf(os.Stderr, "error %s: %v\n", res.path, res.err)
			continue
		}
		if len(res.lapIdx) < 2 {
			fmt.Fprintf(os.Stderr, "warning: no laps detected for %s, skipping\n", res.path)
			continue
		}

		if res.race == "sprint" {
			detectedSprint = true
		}

		for i := 0; i < len(res.lapIdx)-1; i++ {
			seg := res.track[res.lapIdx[i]:res.lapIdx[i+1]]
			allPoints = append(allPoints, seg...)
			allLapIdx = append(allLapIdx, len(allPoints))
			lapsAdded++
			// In sprint mode, we only want a single pass; break once added.
			if *sprintMode || res.race == "sprint" {
				break
			}
		}

		sessionLogs = append(sessionLogs, fmt.Sprintf("%s laps=%d dist=%.1fm time=%.1fs events=%d", res.path, len(res.lapIdx)-1, res.dist, res.dur, len(res.events)))
		sessions = append(sessions, res)
	}

	if lapsAdded == 0 {
		fmt.Fprintf(os.Stderr, "no laps available from input files\n")
		os.Exit(1)
	}

	for _, s := range sessionLogs {
		fmt.Fprintf(os.Stderr, "session: %s\n", s)
	}
	fmt.Fprintf(os.Stderr, "total laps aggregated: %d\n", lapsAdded)

	trackPoints := allPoints
	lapIdx := allLapIdx

	effectiveSprint := *sprintMode || detectedSprint

	if effectiveSprint {
		if *useMaster {
			master := track.BuildMasterPath(trackPoints, *masterSamples, false)
			if len(master) > 0 {
				masterTrack = master
				trackPoints = master
				lapIdx = []int{0, len(master)}
				fmt.Fprintf(os.Stderr, "using master sprint path (%d points)\n", len(master))
			}
		}
	} else {
		if *useMaster && len(lapIdx) > 1 {
			master := track.BuildMasterLap(trackPoints, lapIdx, *masterSamples)
			if len(master) > 0 {
				masterTrack = master
				trackPoints = master
				lapIdx = []int{0, len(master)}
				fmt.Fprintf(os.Stderr, "using master lap (%d points) from %d input laps\n", len(master), lapsAdded)
			}
		}
	}

	if masterTrack == nil {
		if effectiveSprint {
			masterTrack = track.BuildMasterPath(trackPoints, *masterSamples, false)
		} else {
			masterTrack = track.BuildMasterLap(trackPoints, lapIdx, *masterSamples)
		}
	}
	if len(masterTrack) == 0 {
		fmt.Fprintf(os.Stderr, "master track not available for mapping\n")
		os.Exit(1)
	}

	out := struct {
		Master   []masterOut `json:"master"`
		Heatmap  []heatOut   `json:"heatmap,omitempty"`
		Events   []eventOut  `json:"events,omitempty"`
		Cars     []carOut    `json:"cars,omitempty"`
		RaceType string      `json:"raceType,omitempty"`
	}{}

	for _, p := range masterTrack {
		out.Master = append(out.Master, masterOut{
			RelS: p.S,
			X:    p.X,
			Y:    p.Y,
		})
	}

	// Parallel per-session processing for mapping/metrics/events.
	type partial struct {
		car           carOut
		events        []eventOut
		source        string
		mapped        []track.MappedPoint
		sumSpeed      []float64
		countSpeed    []int
		sumAccel      []float64
		countAccel    []int
		surfaceCounts []map[string]int
		lappedCount   int
		sprintCount   int
	}

	partials := make([]partial, len(sessions))
	var wgSess sync.WaitGroup
	for idx, sess := range sessions {
		wgSess.Add(1)
		go func(i int, sess sessionResult) {
			defer wgSess.Done()
			base := filepath.Base(sess.path)
			sourceName := strings.TrimSuffix(base, filepath.Ext(base))
			res := partial{
				source:        sourceName,
				sumSpeed:      make([]float64, len(masterTrack)),
				countSpeed:    make([]int, len(masterTrack)),
				sumAccel:      make([]float64, len(masterTrack)),
				countAccel:    make([]int, len(masterTrack)),
				surfaceCounts: make([]map[string]int, len(masterTrack)),
			}
			for k := range res.surfaceCounts {
				res.surfaceCounts[k] = make(map[string]int)
			}
			res.car.Source = sourceName
			res.car.RaceType = sess.race
			if sess.race == "sprint" {
				res.sprintCount = 1
			} else if sess.race == "lapped" {
				res.lappedCount = 1
			}
			lapStartTime := make(map[int]float64)
			lapLength := make(map[int]float64)
			lapDeltaOffset := make(map[int]float64)
			surfaceLabels := track.ClassifySurface(sess.samples, sess.track, 30)
			lastSurface := ""
			if len(sess.samples) > 0 {
				for i := range sess.events {
					sess.events[i].Time -= sess.samples[0].Time
					if sess.events[i].Time < 0 {
						sess.events[i].Time = 0
					}
				}
			}
			longAcc := make([]float64, len(sess.samples))
			latAcc := make([]float64, len(sess.samples))
			yawRate := make([]float64, len(sess.samples))
			var maxPosAcc, maxNegAcc float64
			var posSamples, negSamples []float64

			// Prefer direct telemetry values when present.
			useDirectAcc := false
			useDirectYaw := false
			var suspFLSm, suspFRSm, suspRLSm, suspRRSm []float64
			for i := 0; i < len(sess.samples); i++ {
				longAcc[i] = sess.samples[i].AccelX
				latAcc[i] = sess.samples[i].AccelY
				yawRate[i] = sess.samples[i].AngVelY
				if longAcc[i] != 0 {
					useDirectAcc = true
					if longAcc[i] > maxPosAcc {
						maxPosAcc = longAcc[i]
					}
					if longAcc[i] < maxNegAcc {
						maxNegAcc = longAcc[i]
					}
					if longAcc[i] > 0 {
						posSamples = append(posSamples, longAcc[i])
					} else if longAcc[i] < 0 {
						negSamples = append(negSamples, -longAcc[i])
					}
				}
				if yawRate[i] != 0 {
					useDirectYaw = true
				}
			}

			// Fall back to derived values if direct telemetry is unavailable/zeroed.
			if !useDirectAcc {
				maxPosAcc, maxNegAcc = 0, 0
				posSamples, negSamples = nil, nil
				for i := 1; i < len(sess.samples) && i < len(sess.track); i++ {
					dt := sess.samples[i].Time - sess.samples[i-1].Time
					if dt <= 0 {
						continue
					}
					dv := sess.samples[i].Speed - sess.samples[i-1].Speed
					longAcc[i] = dv / dt
					if longAcc[i] > maxPosAcc {
						maxPosAcc = longAcc[i]
					}
					if longAcc[i] < maxNegAcc {
						maxNegAcc = longAcc[i]
					}
					if longAcc[i] > 0 {
						posSamples = append(posSamples, longAcc[i])
					} else if longAcc[i] < 0 {
						negSamples = append(negSamples, -longAcc[i])
					}
				}
			}
			if !useDirectYaw {
				for i := 1; i < len(sess.samples) && i < len(sess.track); i++ {
					dt := sess.samples[i].Time - sess.samples[i-1].Time
					if dt <= 0 {
						continue
					}
					dh := sess.track[i].Theta - sess.track[i-1].Theta
					for dh > math.Pi {
						dh -= 2 * math.Pi
					}
					for dh < -math.Pi {
						dh += 2 * math.Pi
					}
					yawRate[i] = dh / dt
					if latAcc[i] == 0 {
						latAcc[i] = sess.samples[i].Speed * yawRate[i]
					}
				}

				// Smooth suspension travel for display.
				suspFLRaw := make([]float64, len(sess.samples))
				suspFRRaw := make([]float64, len(sess.samples))
				suspRLRaw := make([]float64, len(sess.samples))
				suspRRRaw := make([]float64, len(sess.samples))
				for i := range sess.samples {
					suspFLRaw[i] = sess.samples[i].SuspTravelFL
					suspFRRaw[i] = sess.samples[i].SuspTravelFR
					suspRLRaw[i] = sess.samples[i].SuspTravelRL
					suspRRRaw[i] = sess.samples[i].SuspTravelRR
				}
				suspFLSm = track.SmoothSeries(suspFLRaw, 5)
				suspFRSm = track.SmoothSeries(suspFRRaw, 5)
				suspRLSm = track.SmoothSeries(suspRLRaw, 5)
				suspRRSm = track.SmoothSeries(suspRRRaw, 5)
			}
			scalePos := percentile(posSamples, 0.9)
			if scalePos <= 0 {
				scalePos = maxPosAcc
			}
			if scalePos <= 0 {
				scalePos = 1
			}
			scaleNeg := percentile(negSamples, 0.9)
			if scaleNeg <= 0 {
				scaleNeg = -maxNegAcc
			}
			if scaleNeg <= 0 {
				scaleNeg = scalePos
			}
			res.car.LapTimes = track.ComputeLapMetrics(sess.samples, sess.track, sess.lapIdx, 3)
			bestSectors := bestSectorTimes(res.car.LapTimes)
			for lapNum := 1; lapNum < len(sess.lapIdx); lapNum++ {
				start := sess.lapIdx[lapNum-1]
				end := sess.lapIdx[lapNum]
				if start < 0 || end > len(sess.track) {
					continue
				}
				segment := sess.track[start:end]
				if len(segment) > 0 {
					lapLength[lapNum] = segment[len(segment)-1].S - segment[0].S
					if lapLength[lapNum] <= 0 {
						lapLength[lapNum] = segment[len(segment)-1].S
					}
					lapStartTime[lapNum] = sess.samples[start].Time - sess.samples[0].Time
					if lapStartTime[lapNum] < 0 {
						lapStartTime[lapNum] = 0
					}
				}
				masterLen := masterTrack[len(masterTrack)-1].S
				lapLen := 0.0
				if len(segment) > 0 {
					lapLen = segment[len(segment)-1].S - segment[0].S
				}
				scaleS := 1.0
				if lapLen > 0 && masterLen > 0 {
					scaleS = masterLen / lapLen
				}
				track.MapToMaster(segment, masterTrack, start, scaleS, func(idx int, relS, x, y float64, mi int, mRelS, mx, my, dist float64) {
					var heading, speedMPH, speedKMH float64
					var gear int
					var t float64
					var accel float64
					var lngAcc, ltAcc, yr float64
					var steer float64
					var suspFL, suspFR, suspRL, suspRR float64
					var tempFL, tempFR, tempRL, tempRR float64
					if idx >= 0 && idx < len(sess.track) {
						heading = sess.track[idx].Theta
						yr = yawRate[idx]
					}
					if idx >= 0 && idx < len(sess.samples) {
						speedMPH = sess.samples[idx].SpeedMPH
						speedKMH = sess.samples[idx].SpeedKMH
						gear = sess.samples[idx].Gear
						t = sess.samples[idx].Time - sess.samples[0].Time
						lngAcc = longAcc[idx]
						ltAcc = latAcc[idx]
						if idx < len(suspFLSm) {
							suspFL = suspFLSm[idx]
						} else {
							suspFL = sess.samples[idx].SuspTravelFL
						}
						if idx < len(suspFRSm) {
							suspFR = suspFRSm[idx]
						} else {
							suspFR = sess.samples[idx].SuspTravelFR
						}
						if idx < len(suspRLSm) {
							suspRL = suspRLSm[idx]
						} else {
							suspRL = sess.samples[idx].SuspTravelRL
						}
						if idx < len(suspRRSm) {
							suspRR = suspRRSm[idx]
						} else {
							suspRR = sess.samples[idx].SuspTravelRR
						}
						tempFL = toCelsius(sess.samples[idx].TireTempFL)
						tempFR = toCelsius(sess.samples[idx].TireTempFR)
						tempRL = toCelsius(sess.samples[idx].TireTempRL)
						tempRR = toCelsius(sess.samples[idx].TireTempRR)
						// Prefer accel from speed delta over time; fallback to telemetry longitudinal accel.
						if idx > 0 {
							prev := sess.samples[idx-1]
							dt := sess.samples[idx].Time - prev.Time
							if dt > 0 {
								dv := sess.samples[idx].Speed - prev.Speed
								accel = dv / dt
							}
						}
						if accel == 0 {
							accel = lngAcc
						}
					}
					if speedMPH == 0 && speedKMH > 0 {
						speedMPH = speedKMH * 0.621371
					}
					if speedMPH == 0 && idx >= 0 && idx < len(sess.samples) {
						speedMPH = sess.samples[idx].Speed * 2.23694
						speedKMH = speedMPH * 1.60934
					}
					if speedKMH == 0 && speedMPH > 0 {
						speedKMH = speedMPH * 1.60934
					}
					if mi >= 0 && mi < len(res.sumSpeed) {
						res.sumSpeed[mi] += speedMPH
						res.countSpeed[mi]++
						if accel != 0 {
							res.sumAccel[mi] += accel
							res.countAccel[mi]++
						}
						if idx >= 0 && idx < len(surfaceLabels) {
							res.surfaceCounts[mi][surfaceLabels[idx]]++
						}
					}
					delta := 0.0
					if lapStart, ok := lapStartTime[lapNum]; ok {
						elapsedLap := t - lapStart
						expected := expectedTimeForProgress(bestSectors, lapLength[lapNum], relS)
						delta = elapsedLap - expected
						if _, seen := lapDeltaOffset[lapNum]; !seen {
							lapDeltaOffset[lapNum] = delta
						}
						delta -= lapDeltaOffset[lapNum]
					}
					currentSurface := ""
					if idx >= 0 && idx < len(surfaceLabels) {
						currentSurface = surfaceLabels[idx]
					}
					clamp01 := func(v float64) float64 {
						if v < 0 {
							return 0
						}
						if v > 1 {
							return 1
						}
						if math.IsNaN(v) || math.IsInf(v, 0) {
							return 0
						}
						return v
					}
					clampSym := func(v float64) float64 {
						if math.IsNaN(v) || math.IsInf(v, 0) {
							return 0
						}
						if v > 1 {
							return 1
						}
						if v < -1 {
							return -1
						}
						return v
					}
					throttle := clamp01(lngAcc / scalePos)
					brake := clamp01(-lngAcc / scaleNeg)
					if lngAcc >= 0 {
						brake = 0
					}
					if lngAcc <= 0 {
						throttle = math.Max(0, throttle)
					}
					var throttleInput, brakeInput, steerInput float64
					if idx >= 0 && idx < len(sess.samples) {
						sample := sess.samples[idx]
						if sample.HasInputAccel || sample.HasInputBrake {
							throttleInput = clamp01(float64(sample.ThrottleRaw) / 255.0)
							brakeInput = clamp01(float64(sample.Brake) / 255.0)
							throttle = throttleInput
							brake = brakeInput
						}
						if sample.HasInputSteer {
							steerInput = clampSym(float64(sample.Steer) / 127.0)
							steer = float64(sample.Steer)
						} else {
							steer = 0
						}
					}
					res.car.Points = append(res.car.Points, carPoint{
						Time:          t,
						Lap:           lapNum,
						RelS:          relS,
						Heading:       heading,
						MasterX:       mx,
						MasterY:       my,
						SpeedMPH:      speedMPH,
						SpeedKMH:      speedKMH,
						Gear:          gear,
						Delta:         delta,
						LongAcc:       lngAcc,
						LatAcc:        ltAcc,
						YawRate:       yr,
						YawDegS:       yr * 180 / math.Pi,
						Throttle:      throttle,
						Brake:         brake,
						SteerDeg:      steer,
						ThrottleInput: throttleInput,
						BrakeInput:    brakeInput,
						SteerInput:    steerInput,
						SuspFL:        suspFL,
						SuspFR:        suspFR,
						SuspRL:        suspRL,
						SuspRR:        suspRR,
						TireTempFL:    tempFL,
						TireTempFR:    tempFR,
						TireTempRL:    tempRL,
						TireTempRR:    tempRR,
					})
					if currentSurface != "" && currentSurface != lastSurface {
						res.events = append(res.events, eventOut{
							Type:    "surface",
							Source:  sourceName,
							Time:    t,
							Note:    fmt.Sprintf("surface change %s", currentSurface),
							Lap:     lapNum,
							RelS:    relS,
							MasterX: mx,
							MasterY: my,
						})
						lastSurface = currentSurface
					}
					res.mapped = append(res.mapped, track.MappedPoint{
						Time:    t,
						Lap:     lapNum,
						RelS:    relS,
						MasterX: mx,
						MasterY: my,
					})
				})
			}

			for _, ev := range sess.events {
				if ev.Index < 0 || ev.Index >= len(sess.track) {
					continue
				}
				lapNum, relS := track.FindLapAndRelS(sess.lapIdx, sess.track, ev.Index)
				px, py := sess.track[ev.Index].X, sess.track[ev.Index].Y
				mi, mRelS, mx, my, dist := track.MapRelSToMaster(masterTrack, relS, px, py)
				eo := eventOut{
					Type:       ev.Type,
					Source:     sourceName,
					Index:      ev.Index,
					Time:       ev.Time,
					Note:       ev.Note,
					Lap:        lapNum,
					RelS:       relS,
					MasterIdx:  mi,
					MasterRelS: mRelS,
					MasterX:    mx,
					MasterY:    my,
					DistanceSq: dist,
				}
				res.events = append(res.events, eo)
			}
			partials[i] = res
		}(idx, sess)
	}
	wgSess.Wait()

	sumSpeed := make([]float64, len(masterTrack))
	countSpeed := make([]int, len(masterTrack))
	sumAccel := make([]float64, len(masterTrack))
	countAccel := make([]int, len(masterTrack))
	surfaceCounts := make([]map[string]int, len(masterTrack))
	for i := range surfaceCounts {
		surfaceCounts[i] = make(map[string]int)
	}
	mapped := make(map[string][]track.MappedPoint)
	lappedCount := 0
	sprintCount := 0

	for _, pr := range partials {
		if pr.car.Source != "" {
			out.Cars = append(out.Cars, pr.car)
		}
		out.Events = append(out.Events, pr.events...)
		if len(pr.mapped) > 0 {
			mapped[pr.source] = append(mapped[pr.source], pr.mapped...)
		}
		for i := range sumSpeed {
			sumSpeed[i] += pr.sumSpeed[i]
			countSpeed[i] += pr.countSpeed[i]
			sumAccel[i] += pr.sumAccel[i]
			countAccel[i] += pr.countAccel[i]
			for k, v := range pr.surfaceCounts[i] {
				surfaceCounts[i][k] += v
			}
		}
		lappedCount += pr.lappedCount
		sprintCount += pr.sprintCount
	}

	// Detect overtakes across cars using mapped points.
	if len(mapped) > 1 {
		for _, ov := range track.DetectOvertakes(mapped) {
			out.Events = append(out.Events, eventOut{
				Type:    "overtake",
				Source:  ov.Source,
				Target:  ov.Target,
				Time:    ov.Time,
				Note:    fmt.Sprintf("%s passed %s", ov.Source, ov.Target),
				Lap:     ov.Lap,
				RelS:    ov.RelS,
				MasterX: ov.MasterX,
				MasterY: ov.MasterY,
			})
		}
	}

	// Sort events by time to make the viewer list ordered.
	sort.Slice(out.Events, func(i, j int) bool {
		return out.Events[i].Time < out.Events[j].Time
	})

	if lappedCount == 0 && sprintCount > 0 {
		out.RaceType = "sprint"
	} else if lappedCount > 0 {
		out.RaceType = "lapped"
	}

	// Build heatmap averages
	for i, p := range masterTrack {
		if countSpeed[i] == 0 {
			continue
		}
		surface := ""
		if len(surfaceCounts[i]) > 0 {
			maxCnt := 0
			for k, v := range surfaceCounts[i] {
				if v > maxCnt {
					maxCnt = v
					surface = k
				}
			}
		}
		out.Heatmap = append(out.Heatmap, heatOut{
			Index: i,
			RelS:  p.S,
			X:     p.X,
			Y:     p.Y,
			AvgAccel: func() float64 {
				if countAccel[i] == 0 {
					return 0
				}
				return sumAccel[i] / float64(countAccel[i])
			}(),
			Surface: surface,
		})
		if i < len(out.Master) {
			out.Master[i].Surface = surface
		}
	}

	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding json: %v\n", err)
		os.Exit(1)
	}

	// Output handling
	if *serve {
		target := *outPath
		if target == "" {
			target = filepath.Join("web", "data.json")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error creating output dir: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(target, []byte(buf.String()), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", target, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", target, len(buf.String()))
		serveViewer(*addr, "web", target)
		return
	}

	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(buf.String()), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *outPath, err)
			os.Exit(1)
		}
		return
	}

	io.WriteString(os.Stdout, buf.String())
}

type sessionResult struct {
	path    string
	track   []models.Trackpoint
	samples []models.Sample
	events  []models.Event
	race    string
	lapIdx  []int
	dist    float64
	dur     float64
	err     error
}

func filesFromFolders(folders []string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, dir := range folders {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(d.Name()), ".csv") {
				if _, ok := seen[path]; !ok {
					out = append(out, path)
					seen[path] = struct{}{}
				}
			}
			return nil
		})
	}
	return out
}

func serveViewer(addr, webDir, dataPath string) {
	absWeb, _ := filepath.Abs(webDir)
	absData, _ := filepath.Abs(dataPath)
	http.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, absData)
	})
	fs := http.FileServer(http.Dir(absWeb))
	http.Handle("/", fs)
	fmt.Fprintf(os.Stderr, "serving %s and %s at http://localhost%s\n", absWeb, absData, addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// bestSectorTimes returns the best sector times per index from lap metrics.
func bestSectorTimes(laps []track.LapMetrics) []float64 {
	var best []float64
	for _, lm := range laps {
		for i, t := range lm.SectorTime {
			if t <= 0 {
				continue
			}
			if i >= len(best) {
				best = append(best, t)
			} else if best[i] == 0 || t < best[i] {
				best[i] = t
			}
		}
	}
	return best
}

func expectedTimeForProgress(best []float64, lapLen, relS float64) float64 {
	if lapLen <= 0 || len(best) == 0 {
		return 0
	}
	sectorLen := lapLen / float64(len(best))
	idx := int(relS / sectorLen)
	if idx >= len(best) {
		idx = len(best) - 1
	}
	inSector := relS - float64(idx)*sectorLen
	expected := 0.0
	for i := 0; i < idx; i++ {
		expected += best[i]
	}
	if sectorLen > 0 {
		expected += best[idx] * (inSector / sectorLen)
	}
	return expected
}

// pointAtTime returns interpolated carPoint at time t.
func pointAtTime(points []carPoint, t float64) (carPoint, bool) {
	if len(points) == 0 {
		return carPoint{}, false
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
	return carPoint{
		Time:     t,
		Lap:      p1.Lap,
		RelS:     p1.RelS + (p2.RelS-p1.RelS)*alpha,
		Heading:  p1.Heading + (p2.Heading-p1.Heading)*alpha,
		MasterX:  p1.MasterX + (p2.MasterX-p1.MasterX)*alpha,
		MasterY:  p1.MasterY + (p2.MasterY-p1.MasterY)*alpha,
		SpeedMPH: p1.SpeedMPH + (p2.SpeedMPH-p1.SpeedMPH)*alpha,
		SpeedKMH: p1.SpeedKMH + (p2.SpeedKMH-p1.SpeedKMH)*alpha,
		Gear:     p1.Gear,
		Delta:    p1.Delta + (p2.Delta-p1.Delta)*alpha,
	}, true
}

func toCelsius(tempF float64) float64 {
	return (tempF - 32.0) * 5.0 / 9.0
}

func percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	idx := p * float64(len(s)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if hi <= lo {
		return s[lo]
	}
	alpha := idx - float64(lo)
	return s[lo]*(1-alpha) + s[hi]*alpha
}

func LoadSamplesFromCSV(path string) ([]models.Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)

	// Read header
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}

	// Lowercase column mapping
	cols := make(map[string]int)
	for i, h := range headers {
		cols[strings.ToLower(h)] = i
	}

	required := []string{"timestampms", "speed_mps", "accel_x", "accel_y", "accel_z", "vel_x", "vel_y", "vel_z"}
	for _, r := range required {
		if _, ok := cols[r]; !ok {
			return nil, fmt.Errorf("missing required column: %s", r)
		}
	}

	// Load rows
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	samples := make([]models.Sample, 0, len(rows))

	parseOrZero := func(s string) float64 {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0
		}
		return v
	}

	getFloat := func(row []string, name string) float64 {
		if idx, ok := cols[name]; ok && idx < len(row) {
			return parseOrZero(row[idx])
		}
		return 0
	}

	getInt := func(row []string, name string) (int, bool) {
		if idx, ok := cols[name]; ok && idx < len(row) {
			val := strings.TrimSpace(strings.ToLower(row[idx]))
			switch val {
			case "true", "1", "yes", "on":
				return 1, true
			case "false", "0", "no", "off":
				return 0, true
			}
			return int(parseOrZero(row[idx])), true
		}
		return 0, false
	}

	getString := func(row []string, name string) string {
		if idx, ok := cols[name]; ok && idx < len(row) {
			return row[idx]
		}
		return ""
	}

	for _, row := range rows {
		timeMS := getFloat(row, "timestampms")
		timeSec := timeMS / 1000.0

		ax := getFloat(row, "accel_x")
		ay := getFloat(row, "accel_y")
		az := getFloat(row, "accel_z")

		vx := getFloat(row, "vel_x")
		vy := getFloat(row, "vel_y")
		vz := getFloat(row, "vel_z")

		speedMPS := getFloat(row, "speed_mps")
		speedKMH := getFloat(row, "speed_kph")
		speedMPH := getFloat(row, "speed_mph")
		if speedMPS == 0 && speedKMH > 0 {
			speedMPS = speedKMH / 3.6
		}
		if speedMPS == 0 && speedMPH > 0 {
			speedMPS = speedMPH / 2.23694
		}
		if speedKMH == 0 && speedMPS > 0 {
			speedKMH = speedMPS * 3.6
		}
		if speedMPH == 0 && speedMPS > 0 {
			speedMPH = speedMPS * 2.23694
		}

		gear, _ := getInt(row, "gear")
		isRaceOn, hasIsRaceOn := getInt(row, "israceon")
		if _, ok := cols["israceon"]; !ok {
			isRaceOn = 1
		} else if isRaceOn != 0 {
			isRaceOn = 1
		}
		throttleRaw, hasAccel := getInt(row, "accel")
		brakeRaw, hasBrake := getInt(row, "brake")
		steerRaw, hasSteer := getInt(row, "steer")

		s := models.Sample{
			CarState: models.CarState{
				Timestamp:           getString(row, "timestamp"),
				IsRaceOn:            isRaceOn,
				TimestampMS:         timeMS,
				EngineMaxRPM:        getFloat(row, "engine_max_rpm"),
				EngineIdleRPM:       getFloat(row, "engine_idle_rpm"),
				EngineCurrentRPM:    getFloat(row, "engine_current_rpm"),
				AccelX:              ax,
				AccelY:              ay,
				AccelZ:              az,
				VelX:                vx,
				VelY:                vy,
				VelZ:                vz,
				AngVelX:             getFloat(row, "ang_vel_x"),
				AngVelY:             getFloat(row, "ang_vel_y"),
				AngVelZ:             getFloat(row, "ang_vel_z"),
				Yaw:                 getFloat(row, "yaw"),
				Pitch:               getFloat(row, "pitch"),
				Roll:                getFloat(row, "roll"),
				NormSuspFL:          getFloat(row, "norm_susp_fl"),
				NormSuspFR:          getFloat(row, "norm_susp_fr"),
				NormSuspRL:          getFloat(row, "norm_susp_rl"),
				NormSuspRR:          getFloat(row, "norm_susp_rr"),
				TireSlipFL:          getFloat(row, "tire_slip_fl"),
				TireSlipFR:          getFloat(row, "tire_slip_fr"),
				TireSlipRL:          getFloat(row, "tire_slip_rl"),
				TireSlipRR:          getFloat(row, "tire_slip_rr"),
				WheelRotFL:          getFloat(row, "wheel_rot_fl"),
				WheelRotFR:          getFloat(row, "wheel_rot_fr"),
				WheelRotRL:          getFloat(row, "wheel_rot_rl"),
				WheelRotRR:          getFloat(row, "wheel_rot_rr"),
				WheelOnRumbleFL:     getFloat(row, "wheel_on_rumble_fl"),
				WheelOnRumbleFR:     getFloat(row, "wheel_on_rumble_fr"),
				WheelOnRumbleRL:     getFloat(row, "wheel_on_rumble_rl"),
				WheelOnRumbleRR:     getFloat(row, "wheel_on_rumble_rr"),
				WheelInPuddleFL:     getFloat(row, "wheel_in_puddle_fl"),
				WheelInPuddleFR:     getFloat(row, "wheel_in_puddle_fr"),
				WheelInPuddleRL:     getFloat(row, "wheel_in_puddle_rl"),
				WheelInPuddleRR:     getFloat(row, "wheel_in_puddle_rr"),
				SurfaceRumbleFL:     getFloat(row, "surface_rumble_fl"),
				SurfaceRumbleFR:     getFloat(row, "surface_rumble_fr"),
				SurfaceRumbleRL:     getFloat(row, "surface_rumble_rl"),
				SurfaceRumbleRR:     getFloat(row, "surface_rumble_rr"),
				TireSlipAngleFL:     getFloat(row, "tire_slip_angle_fl"),
				TireSlipAngleFR:     getFloat(row, "tire_slip_angle_fr"),
				TireSlipAngleRL:     getFloat(row, "tire_slip_angle_rl"),
				TireSlipAngleRR:     getFloat(row, "tire_slip_angle_rr"),
				TireCombinedSlipFL:  getFloat(row, "tire_combined_slip_fl"),
				TireCombinedSlipFR:  getFloat(row, "tire_combined_slip_fr"),
				TireCombinedSlipRL:  getFloat(row, "tire_combined_slip_rl"),
				TireCombinedSlipRR:  getFloat(row, "tire_combined_slip_rr"),
				SuspTravelFL:        getFloat(row, "susp_travel_fl"),
				SuspTravelFR:        getFloat(row, "susp_travel_fr"),
				SuspTravelRL:        getFloat(row, "susp_travel_rl"),
				SuspTravelRR:        getFloat(row, "susp_travel_rr"),
				CarOrdinal:          int(getFloat(row, "car_ordinal")),
				CarClass:            int(getFloat(row, "car_class")),
				CarPerformanceIndex: int(getFloat(row, "car_performance_index")),
				DrivetrainType:      int(getFloat(row, "drivetrain_type")),
				NumCylinders:        int(getFloat(row, "num_cylinders")),
				PosX:                getFloat(row, "pos_x"),
				PosY:                getFloat(row, "pos_y"),
				PosZ:                getFloat(row, "pos_z"),
				SpeedMPS:            speedMPS,
				SpeedKMH:            speedKMH,
				SpeedMPH:            speedMPH,
				Power:               getFloat(row, "power"),
				Torque:              getFloat(row, "torque"),
				TireTempFL:          getFloat(row, "tire_temp_fl"),
				TireTempFR:          getFloat(row, "tire_temp_fr"),
				TireTempRL:          getFloat(row, "tire_temp_rl"),
				TireTempRR:          getFloat(row, "tire_temp_rr"),
				Boost:               getFloat(row, "boost"),
				Fuel:                getFloat(row, "fuel"),
				Distance:            getFloat(row, "distance"),
				BestLap:             getFloat(row, "best_lap"),
				LastLap:             getFloat(row, "last_lap"),
				CurrentLap:          getFloat(row, "current_lap"),
				CurrentRaceTime:     getFloat(row, "current_race_time"),
				LapNumber:           int(getFloat(row, "lap_number")),
				RacePosition:        int(getFloat(row, "race_position")),
				ThrottleRaw:         throttleRaw,
				Brake:               brakeRaw,
				Clutch:              int(getFloat(row, "clutch")),
				Handbrake:           int(getFloat(row, "handbrake")),
				Gear:                gear,
				Steer:               steerRaw,
				NormDrivingLine:     int(getFloat(row, "norm_driving_line")),
				NormAIBrakeDiff:     int(getFloat(row, "norm_ai_brake_diff")),
			},
			Time:          timeSec,
			Speed:         speedMPS,
			SmoothAx:      ax, // seed smoother with raw value
			HasInputAccel: hasAccel,
			HasInputBrake: hasBrake,
			HasInputSteer: hasSteer,
		}
		if hasIsRaceOn {
			s.IsRaceOn = isRaceOn
		}
		if s.IsRaceOn == 0 {
			continue
		}

		samples = append(samples, s)
	}

	return samples, nil
}

type multiFlag []string

func (m *multiFlag) String() string {
	if m == nil {
		return ""
	}
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
