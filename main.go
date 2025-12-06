package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"forza/models"
	"forza/track"
	"math"
	"os"
	"strconv"
	"strings"
)

func main() {

	var filePaths multiFlag
	flag.Var(&filePaths, "file", "Path to telemetry CSV file (repeatable)")
	lapLen := flag.Float64("lap-length", 0, "Expected lap length in meters (0 to autodetect by start crossing)")
	lapTol := flag.Float64("lap-tol", 25, "Tolerance for lap length matching (meters)")
	lapCount := flag.Int("lap-count", 0, "Known lap count; with lap-length=0, lap length is estimated as total distance / lap-count")
	minLapSpacing := flag.Float64("min-lap-spacing", 200, "Minimum distance (m) between lap boundaries when using distance-based detection")
	outputLaps := flag.Bool("lap-output", true, "When true, output Lap,RelS,X,Y columns using detected lap splits")
	lapSelect := flag.Int("lap-select", 0, "If >0, only output this lap number (1-based) instead of all laps")
	masterSamples := flag.Int("master-samples", 2000, "Resampled points per lap when building master lap")
	useMaster := flag.Bool("use-master", true, "If true, output the averaged master lap instead of per-lap raw points")
	flag.Parse()

	if len(filePaths) == 0 {
		filePaths = append(filePaths, "mockdata/gol1/Car-5030.csv")
	}

	var (
		allPoints   []models.Trackpoint
		allLapIdx   []int
		totalDist   float64
		totalTime   float64
		lapsAdded   int
		sessionLogs []string
	)
	allLapIdx = append(allLapIdx, 0)

	for _, path := range filePaths {
		samples, err := LoadSamplesFromCSV(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading CSV %s: %v\n", path, err)
			continue
		}

		trackPoints, err := track.BuildTrack(samples)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error building track for %s: %v\n", path, err)
			continue
		}

		sessionDist := trackPoints[len(trackPoints)-1].S
		sessionTime := samples[len(samples)-1].Time - samples[0].Time
		laps := deriveLapCount(sessionDist, *lapCount)
		lapIdx := buildLapIdx(trackPoints, laps, *lapLen, *lapTol, *minLapSpacing)

		if len(lapIdx) < 2 {
			fmt.Fprintf(os.Stderr, "warning: no laps detected for %s, skipping\n", path)
			continue
		}

		for i := 0; i < len(lapIdx)-1; i++ {
			seg := trackPoints[lapIdx[i]:lapIdx[i+1]]
			seg = track.NormalizeLapSegment(seg)
			allPoints = append(allPoints, seg...)
			allLapIdx = append(allLapIdx, len(allPoints))
			lapsAdded++
		}

		totalDist += sessionDist
		totalTime += sessionTime
		sessionLogs = append(sessionLogs, fmt.Sprintf("%s laps=%d dist=%.1fm time=%.1fs", path, len(lapIdx)-1, sessionDist, sessionTime))
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
			trackPoints = master
			lapIdx = []int{0, len(master)}
			fmt.Fprintf(os.Stderr, "using master lap (%d points) from %d input laps\n", len(master), lapsAdded)
		}
	}

	if *lapSelect > 0 && *lapSelect < len(lapIdx) {
		start := lapIdx[*lapSelect-1]
		end := lapIdx[*lapSelect]
		lapIdx = []int{start, end}
	}

	if *outputLaps {
		fmt.Println("Lap,RelS,X,Y")
		for lapNum := 1; lapNum < len(lapIdx); lapNum++ {
			start := lapIdx[lapNum-1]
			end := lapIdx[lapNum]
			startS := trackPoints[start].S
			for i := start; i < end; i++ {
				relS := trackPoints[i].S - startS
				fmt.Printf("%d,%.6f,%.6f,%.6f\n", lapNum, relS, trackPoints[i].X, trackPoints[i].Y)
			}
		}
	} else {
		fmt.Println("X,Y")
		for _, p := range trackPoints {
			fmt.Printf("%.6f,%.6f\n", p.X, p.Y)
		}
	}
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

		samples = append(samples, models.Sample{
			Time:     timeSec,
			Speed:    speed,
			AccelX:   ax,
			AccelY:   ay,
			AccelZ:   az,
			VelX:     vx,
			VelY:     vy,
			VelZ:     vz,
			SmoothAx: ax, // seed smoother with raw value
		})
	}

	return samples, nil
}

// buildEvenLapIdx generates lap boundaries assuming 'laps' equally spaced laps
// by distance across the session.
func buildEvenLapIdx(points []models.Trackpoint, laps int) []int {
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
	// Ensure final endpoint (end-exclusive) is included.
	if out[len(out)-1] != len(points) {
		out = append(out, len(points))
	}
	return out
}

func deriveLapCount(totalDist float64, preferred int) int {
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

func buildLapIdx(trackPoints []models.Trackpoint, laps int, lapLen float64, lapTol float64, minLapSpacing float64) []int {
	if lapLen > 0 && laps <= 1 {
		idx := track.FindLapIndicesByDistanceWithMin(trackPoints, lapLen, lapTol, math.Max(minLapSpacing, lapLen*0.2))
		if len(idx) >= 2 {
			return idx
		}
	}
	return buildEvenLapIdx(trackPoints, laps)
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
