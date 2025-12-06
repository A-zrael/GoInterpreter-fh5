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
	SmoothAx float64
}

type Trackpoint struct {
	S     float64
	X     float64
	Y     float64
	Theta float64
}
