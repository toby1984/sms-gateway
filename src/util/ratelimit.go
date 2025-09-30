package util

import "strconv"

type RateLimit struct {
	Threshold int
	Interval  TimeInterval
}

func (r *RateLimit) String() string {
	return strconv.Itoa(r.Threshold) + " / " + r.Interval.String()
}

func (r *RateLimit) IsThresholdExceeded(value int) bool {
	return value > r.Threshold
}
