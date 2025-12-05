package main

import (
	"encoding/csv"
	"fmt"
	"forza/models"
	"forza/track"
	"os"
	"strconv"
	"strings"
)

func main() {

	nathan, err := LoadSamplesFromCSV("./gol1/Car-5030.csv")
	if err != nil {
		fmt.Println("error loading CSV")
		return
	}

	samplesloaded, err := track.BuildTrack(nathan)
	if err != nil {
		panic(err)
	}

	fmt.Println("X,Y")
	for _, p := range samplesloaded {
		fmt.Printf("%.6f,%.6f\n", p.X, p.Y)
	}
	// dump to CSV / stdout
	for _, p := range samplesloaded {
		fmt.Printf("%.6f,%.6f\n", p.X, p.Y)
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

	for _, row := range rows {
		// Convert int timestamp to seconds
		timestampMS, _ := strconv.ParseFloat(row[cols["timestampms"]], 64)

		speed, _ := strconv.ParseFloat(row[cols["speed_mps"]], 64)

		ax, _ := strconv.ParseFloat(row[cols["accel_x"]], 64)

		samples = append(samples, models.Sample{
			Time:     timestampMS,
			Speed:    speed,
			AccelX:   ax,
			SmoothAx: ax,
		})
	}

	return samples, nil
}
