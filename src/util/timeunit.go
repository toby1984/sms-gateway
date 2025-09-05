package util

import "errors"

type TimeUnit int

const (
	Seconds TimeUnit = iota
	Minutes
	Hours
	Days
	Weeks
)

func (d TimeUnit) String() string {
	switch d {
	case Seconds:
		return "Second"
	case Minutes:
		return "Minute"
	case Hours:
		return "Hour"
	case Days:
		return "Day"
	case Weeks:
		return "Week"
	default:
		return "Unknown"
	}
}

func StringToTimeUnit(unit string) (TimeUnit, error) {
	switch unit {
	case "s":
		return Seconds, nil
	case "m":
		return Minutes, nil
	case "h":
		return Hours, nil
	case "d":
		return Days, nil
	case "w":
		return Weeks, nil
	default:
		return 0, errors.New("Invalid time unit: '" + unit + "'")
	}
}
