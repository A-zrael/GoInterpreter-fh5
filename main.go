package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"forza/models"
	"forza/track"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func main() {
	var filePaths multiFlag
	flag.Var(&filePaths, "file", "Path to telemetry CSV file (repeatable)")
	var folderPaths multiFlag
	flag.Var(&folderPaths, "folder", "Folder containing telemetry CSV files (repeatable, recursive)")
	lapLen := flag.Float64("lap-length", 0, "Expected lap length in meters (0 to autodetect by start crossing)")
	lapTol := flag.Float64("lap-tol", 25, "Tolerance for lap length matching (meters)")
	lapCount := flag.Int("lap-count", 0, "Known lap count; with lap-length=0, lap length is estimated as total distance / lap-count")
	minLapSpacing := flag.Float64("min-lap-spacing", 200, "Minimum distance (m) between lap boundaries when using distance-based detection")
	masterSamples := flag.Int("master-samples", 2000, "Resampled points per lap when building master lap")
	useMaster := flag.Bool("use-master", true, "If true, output the averaged master lap instead of per-lap raw points")
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
		allPoints   []models.Trackpoint
		allLapIdx   []int
		lapsAdded   int
		sessionLogs []string
		masterTrack []models.Trackpoint
		sessions    []sessionResult
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
			tp, err := track.BuildTrack(samples)
			if err != nil {
				results <- sessionResult{path: p, err: fmt.Errorf("track: %w", err)}
				return
			}
			events := track.DetectEvents(samples)
			sessionDist := tp[len(tp)-1].S
			sessionTime := samples[len(samples)-1].Time - samples[0].Time
			laps := track.DeriveLapCount(sessionDist, *lapCount)
			lapIdx := track.BuildLapIdx(tp, laps, *lapLen, *lapTol, *minLapSpacing)
			results <- sessionResult{
				path:    p,
				track:   tp,
				samples: samples,
				events:  events,
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

		for i := 0; i < len(res.lapIdx)-1; i++ {
			seg := res.track[res.lapIdx[i]:res.lapIdx[i+1]]
			seg = track.NormalizeLapSegment(seg)
			allPoints = append(allPoints, seg...)
			allLapIdx = append(allLapIdx, len(allPoints))
			lapsAdded++
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

	if *useMaster && len(lapIdx) > 1 {
		master := track.BuildMasterLap(trackPoints, lapIdx, *masterSamples)
		if len(master) > 0 {
			masterTrack = master
			trackPoints = master
			lapIdx = []int{0, len(master)}
			fmt.Fprintf(os.Stderr, "using master lap (%d points) from %d input laps\n", len(master), lapsAdded)
		}
	}

	if masterTrack == nil {
		masterTrack = track.BuildMasterLap(trackPoints, lapIdx, *masterSamples)
	}
	if len(masterTrack) == 0 {
		fmt.Fprintf(os.Stderr, "master track not available for mapping\n")
		os.Exit(1)
	}

	type masterOut struct {
		RelS float64 `json:"relS"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
	}
	type eventOut struct {
		Type       string  `json:"type"`
		Source     string  `json:"source"`
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
		Heading  float64 `json:"heading"`
		MasterX  float64 `json:"masterX"`
		MasterY  float64 `json:"masterY"`
		SpeedMPH float64 `json:"speedMPH"`
		SpeedKMH float64 `json:"speedKMH"`
		Gear     int     `json:"gear"`
	}
	type carOut struct {
		Source   string             `json:"source"`
		Points   []carPoint         `json:"points,omitempty"`
		LapTimes []track.LapMetrics `json:"lapTimes,omitempty"`
	}
	out := struct {
		Master []masterOut `json:"master"`
		Events []eventOut  `json:"events,omitempty"`
		Cars   []carOut    `json:"cars,omitempty"`
	}{}

	for _, p := range masterTrack {
		out.Master = append(out.Master, masterOut{
			RelS: p.S,
			X:    p.X,
			Y:    p.Y,
		})
	}

	for _, sess := range sessions {
		cOut := carOut{Source: sess.path}

		if len(sess.samples) > 0 {
			// Normalize event times to session start so they align with point times.
			for i := range sess.events {
				sess.events[i].Time -= sess.samples[0].Time
				if sess.events[i].Time < 0 {
					sess.events[i].Time = 0
				}
			}
		}
		// Lap times with embedded sector splits (3 sectors by default).
		cOut.LapTimes = track.ComputeLapMetrics(sess.samples, sess.track, sess.lapIdx, 3)
		for lapNum := 1; lapNum < len(sess.lapIdx); lapNum++ {
			start := sess.lapIdx[lapNum-1]
			end := sess.lapIdx[lapNum]
			if start < 0 || end > len(sess.track) {
				continue
			}
			segment := sess.track[start:end]
			track.MapToMaster(segment, masterTrack, start, func(idx int, relS, x, y float64, mi int, mRelS, mx, my, dist float64) {
				var heading, speedMPH, speedKMH float64
				var gear int
				var t float64
				if idx >= 0 && idx < len(sess.track) {
					heading = sess.track[idx].Theta
				}
				if idx >= 0 && idx < len(sess.samples) {
					speedMPH = sess.samples[idx].SpeedMPH
					speedKMH = sess.samples[idx].SpeedKMH
					gear = sess.samples[idx].Gear
					t = sess.samples[idx].Time - sess.samples[0].Time
				}
				cOut.Points = append(cOut.Points, carPoint{
					Time:     t,
					Lap:      lapNum,
					Heading:  heading,
					MasterX:  mx,
					MasterY:  my,
					SpeedMPH: speedMPH,
					SpeedKMH: speedKMH,
					Gear:     gear,
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
				Source:     sess.path,
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
			out.Events = append(out.Events, eo)
		}

		out.Cars = append(out.Cars, cOut)
	}

	// Sort events by time to make the viewer list ordered.
	sort.Slice(out.Events, func(i, j int) bool {
		return out.Events[i].Time < out.Events[j].Time
	})

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding json: %v\n", err)
		os.Exit(1)
	}
}

type sessionResult struct {
	path    string
	track   []models.Trackpoint
	samples []models.Sample
	events  []models.Event
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

	for _, row := range rows {
		// Convert ms timestamp to seconds
		timeSec := parseOrZero(row[cols["timestampms"]]) / 1000.0
		speed := parseOrZero(row[cols["speed_mps"]])

		ax := parseOrZero(row[cols["accel_x"]])
		ay := parseOrZero(row[cols["accel_y"]])
		az := parseOrZero(row[cols["accel_z"]])

		vx := parseOrZero(row[cols["vel_x"]])
		vy := parseOrZero(row[cols["vel_y"]])
		vz := parseOrZero(row[cols["vel_z"]])

		var speedMPH, speedKMH float64
		if idx, ok := cols["speed_mph"]; ok {
			speedMPH = parseOrZero(row[idx])
		}
		if idx, ok := cols["speed_kmh"]; ok {
			speedKMH = parseOrZero(row[idx])
		}

		gear := 0
		if idx, ok := cols["gear"]; ok {
			gear = int(parseOrZero(row[idx]))
		}
		isRaceOn := 1
		if idx, ok := cols["israceon"]; ok {
			val := strings.TrimSpace(strings.ToLower(row[idx]))
			switch val {
			case "true", "1", "yes", "on":
				isRaceOn = 1
			case "false", "0", "no", "off":
				isRaceOn = 0
			default:
				isRaceOn = int(parseOrZero(row[idx]))
			}
		}

		samples = append(samples, models.Sample{
			Time:     timeSec,
			Speed:    speed,
			AccelX:   ax,
			AccelY:   ay,
			AccelZ:   az,
			VelX:     vx,
			VelY:     vy,
			VelZ:     vz,
			IsRaceOn: isRaceOn,
			SpeedMPH: speedMPH,
			SpeedKMH: speedKMH,
			Gear:     gear,
			SmoothAx: ax, // seed smoother with raw value
		})
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
