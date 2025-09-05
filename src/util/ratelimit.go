package util

type RateLimit struct {
	Threshold int
	Interval  TimeInterval
}

func (r *RateLimit) IsThresholdExceeded(value int) bool {
	return value > r.Threshold
}
