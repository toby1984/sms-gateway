package state

import (
	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/logger"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

var log = logger.GetLogger("state")

var appConfig config.Config

type Timestamp int64

type State struct {
	timestamps []Timestamp
}

/**
 * Count SMS within the time frame of [now()-interval,now()]
 */
func (c *State) countSms(iv config.TimeInterval) int {
	now := Timestamp(time.Now().Unix())
	maxTs := iv.ToSeconds()
	smsCount := 0
	for i := len(c.timestamps) - 1; i >= 0; i-- {
		age := int(now - c.timestamps[i])
		if age > maxTs {
			break
		}
		smsCount++
	}
	return smsCount
}

func (c *State) IsAnyRateLimitExceeded() bool {
	if len(c.timestamps) == 0 {
		return false
	}
	if appConfig.GetRateLimit1() != nil {
		var cnt = c.countSms(appConfig.GetRateLimit1().Interval)
		if appConfig.GetRateLimit1().IsThresholdExceeded(cnt) {
			return true
		}
	}
	if appConfig.GetRateLimit2() != nil {
		var cnt = c.countSms(appConfig.GetRateLimit2().Interval)
		if appConfig.GetRateLimit2().IsThresholdExceeded(cnt) {
			return true
		}
	}
	return false
}

func (c *State) RememberSmsSend() {
	now := time.Now().Unix()
	c.timestamps = append(c.timestamps, Timestamp(now))

	var cutOffTimestamp Timestamp = -1
	if appConfig.GetRateLimit1() != nil {
		if appConfig.GetRateLimit2() != nil {
			if appConfig.GetRateLimit1().Interval.IsGreaterThan(&appConfig.GetRateLimit2().Interval) {
				cutOffTimestamp = Timestamp(now - int64(appConfig.GetRateLimit1().Interval.ToSeconds()))
			} else {
				cutOffTimestamp = Timestamp(now - int64(appConfig.GetRateLimit2().Interval.ToSeconds()))
			}
		} else {
			cutOffTimestamp = Timestamp(now - int64(appConfig.GetRateLimit1().Interval.ToSeconds()))
		}
	} else if appConfig.GetRateLimit2() != nil {
		cutOffTimestamp = Timestamp(now - int64(appConfig.GetRateLimit2().Interval.ToSeconds()))
	}

	if cutOffTimestamp != -1 {
		for i := len(c.timestamps) - 1; i >= 0; i-- {
			if c.timestamps[i] < cutOffTimestamp {
				var newLen = len(c.timestamps) - (i + 1)
				c.timestamps = c.timestamps[:newLen]
				break
			}
		}
	}

	// WriteState() will log an error, we'll just swallow this one here
	// hoping that at some future time we'll be able to persist the state again....
	_ = c.WriteState()
}

func getStateFile() string {
	return appConfig.GetDataDirectory() + "/state.txt"
}

func (c *State) WriteState() error {

	var sb strings.Builder
	for idx, ts := range c.timestamps {
		sb.WriteString(strconv.FormatInt(int64(ts), 10))
		if idx+1 < len(c.timestamps) {
			sb.WriteString("\n")
		}
	}
	log.Debug("Persisting application state to " + getStateFile())
	err := os.WriteFile(getStateFile(), []byte(sb.String()), 0644)
	if err != nil {
		msg := "Failed to write state file '" + getStateFile() + "' - " + err.Error()
		log.Error(msg)
		return errors.New(msg)
	}
	return nil
}

func Init(config *config.Config) (*State, error) {
	appConfig = *config

	result := &State{}
	if common.FileExist(getStateFile()) {
		content, err := os.ReadFile(getStateFile())
		if err != nil {
			return nil, errors.New("Failed to read state file: " + getStateFile() + " - " + err.Error())
		}
		lines := strings.Split(string(content), "\n")

		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				var v, err = common.AToInt64(line)
				if err != nil {
					return nil, errors.New("Failed to parse state file: " + getStateFile() + " - invalid line '" + line + "'")
				}
				result.timestamps = append(result.timestamps, Timestamp(v))
			}
		}
	} else {
		log.Info("State file '" + getStateFile() + "' does not exist, creating an empty file")
		err := result.WriteState()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
