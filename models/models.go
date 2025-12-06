package models

type Sample struct {
	Time     float64
	Speed    float64
	AccelX   float64
	AccelY   float64
	AccelZ   float64
	VelX     float64
	VelY     float64
	VelZ     float64
	SpeedMPH float64
	SpeedKMH float64
	Gear     int
	SmoothAx float64
}

type Trackpoint struct {
	S     float64
	X     float64
	Y     float64
	Theta float64
}

type Event struct {
	Index int
	Time  float64
	Type  string
	Note  string
	// Optional spatial mapping to master lap
	MasterIdx  int
	MasterX    float64
	MasterY    float64
	MasterRelS float64
	DistanceSq float64
}
