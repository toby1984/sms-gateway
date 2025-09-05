package util

import (
	"time"
)

type TimeInterval struct {
	Value int
	Unit  TimeUnit
}

func (iv *TimeInterval) IsGreaterThan(other *TimeInterval) bool {
	return iv.Compare(other) > 0
}

func (iv *TimeInterval) IsLessThan(other *TimeInterval) bool {
	return iv.Compare(other) < 0
}

func (iv *TimeInterval) Equals(other *TimeInterval) bool {
	return iv.Compare(other) == 0
}

func (iv *TimeInterval) Compare(other *TimeInterval) int {
	a := iv.ToSeconds()
	b := other.ToSeconds()
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

func (iv *TimeInterval) ToSeconds() int {
	factor := 1
	switch iv.Unit {
	case Seconds:
		factor = 1
	case Minutes:
		factor = 60
	case Hours:
		factor = 60 * 60
	case Days:
		factor = 60 * 60 * 24
	case Weeks:
		factor = 60 * 60 * 24 * 7
	default:
		panic("Internal error, unhandled switch/case: " + iv.Unit.String())
	}
	return iv.Value * factor
}

func (iv *TimeInterval) IsShorterThan(elapsed time.Duration) bool {
	return float64(iv.ToSeconds()) < elapsed.Seconds()
}
