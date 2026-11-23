package player

type Engine struct {
	Open         func(url string, position float64) bool
	Seek         func(seconds float64)
	SetRate      func(rate float64)
	SelectAudio  func(name string) bool
	Stop         func()
	Snapshot     func() Status
	Fullscreen   func(bool)
	Viewport     func(widthFraction float64)
	BackdropBlur func(radius float32) bool
	Capabilities func() Capabilities
}

type Capabilities struct {
	HDR  bool
	HEVC bool
}

type Status struct {
	Position            float64
	Duration            float64
	BufferEnd           float64
	HasFrame            bool
	Buffering           bool
	Ended               bool
	Failed              bool
	DisplayIncompatible bool
	AudioActive         bool
	HDRVideo            bool
	IdleDisabled        bool
	MatchDisplay        bool
	ErrorCode           int64
}
