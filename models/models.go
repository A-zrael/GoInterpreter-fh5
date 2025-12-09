package models

// CarState holds the full Forza telemetry payload as exposed by the game.
type CarState struct {
	Timestamp           string
	IsRaceOn            int
	TimestampMS         float64
	EngineMaxRPM        float64
	EngineIdleRPM       float64
	EngineCurrentRPM    float64
	AccelX              float64
	AccelY              float64
	AccelZ              float64
	VelX                float64
	VelY                float64
	VelZ                float64
	AngVelX             float64
	AngVelY             float64
	AngVelZ             float64
	Yaw                 float64
	Pitch               float64
	Roll                float64
	NormSuspFL          float64
	NormSuspFR          float64
	NormSuspRL          float64
	NormSuspRR          float64
	TireSlipFL          float64
	TireSlipFR          float64
	TireSlipRL          float64
	TireSlipRR          float64
	WheelRotFL          float64
	WheelRotFR          float64
	WheelRotRL          float64
	WheelRotRR          float64
	WheelOnRumbleFL     float64
	WheelOnRumbleFR     float64
	WheelOnRumbleRL     float64
	WheelOnRumbleRR     float64
	WheelInPuddleFL     float64
	WheelInPuddleFR     float64
	WheelInPuddleRL     float64
	WheelInPuddleRR     float64
	SurfaceRumbleFL     float64
	SurfaceRumbleFR     float64
	SurfaceRumbleRL     float64
	SurfaceRumbleRR     float64
	TireSlipAngleFL     float64
	TireSlipAngleFR     float64
	TireSlipAngleRL     float64
	TireSlipAngleRR     float64
	TireCombinedSlipFL  float64
	TireCombinedSlipFR  float64
	TireCombinedSlipRL  float64
	TireCombinedSlipRR  float64
	SuspTravelFL        float64
	SuspTravelFR        float64
	SuspTravelRL        float64
	SuspTravelRR        float64
	CarOrdinal          int
	CarClass            int
	CarPerformanceIndex int
	DrivetrainType      int
	NumCylinders        int
	PosX                float64
	PosY                float64
	PosZ                float64
	SpeedMPS            float64
	SpeedKMH            float64
	SpeedMPH            float64
	Power               float64
	Torque              float64
	TireTempFL          float64
	TireTempFR          float64
	TireTempRL          float64
	TireTempRR          float64
	Boost               float64
	Fuel                float64
	Distance            float64
	BestLap             float64
	LastLap             float64
	CurrentLap          float64
	CurrentRaceTime     float64
	LapNumber           int
	RacePosition        int
	ThrottleRaw         int
	Brake               int
	Clutch              int
	Handbrake           int
	Gear                int
	Steer               int
	NormDrivingLine     int
	NormAIBrakeDiff     int
}

// Sample represents a recorded telemetry sample along with derived data used throughout the pipeline.
type Sample struct {
	CarState

	Time     float64
	Speed    float64
	SmoothAx float64

	HasInputAccel bool
	HasInputBrake bool
	HasInputSteer bool
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
