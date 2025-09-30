package state

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/message"
	"code-sourcery.de/sms-gateway/util"
)

var log = logger.GetLogger("state")

// Unit timestamp, seconds since Epoch
type UnixTimestamp int64

func (t UnixTimestamp) ToTime() time.Time {
	return time.Unix(int64(t), 0)
}

type internalState struct {

	// !!! Make sure to adjust createCopy() when changing this structure
	Timestamps []UnixTimestamp `json:"msg_timestamps"`

	// ID of last message that was successfully sent
	LastSuccessfulMessageId *message.MessageId `json:"last_successful_message_id"`

	// pending message IDs, ordered ascending by creation timestamp
	// (element 0 = oldest message ID)
	PendingMessageIds []message.MessageId `json:"pending_message_ids"`

	// next ID to be used for a new, incoming message
	NextMessageId message.MessageId `json:"next_message_id"`

	LastKeepAliveMsgEnqueued *UnixTimestamp `json:"last_keepalive_msg_enqueued"`
}

type State struct {
	data internalState
}

var appConfig config.Config

// mutex to protect state
var mutex sync.Mutex

func newInternalState() internalState {
	return internalState{NextMessageId: message.FirstMessageId()}
}

/**
 * Count SMS within the time frame of [now()-interval,now()]
 */
func (c *State) countSms(iv util.TimeInterval) int {
	now := UnixTimestamp(time.Now().Unix())
	maxTs := iv.ToSeconds()
	smsCount := 0
	for i := len(c.data.Timestamps) - 1; i >= 0; i-- {
		ageInSeconds := int(now - c.data.Timestamps[i])
		if ageInSeconds > maxTs {
			break
		}
		smsCount++
	}
	return smsCount
}

func (c *State) IsAnyRateLimitExceeded() bool {

	mutex.Lock()
	defer mutex.Unlock()

	if len(c.data.Timestamps) == 0 {
		return false
	}
	if appConfig.GetRateLimit1() != nil {
		var cnt = c.countSms(appConfig.GetRateLimit1().Interval)
		if appConfig.GetRateLimit1().IsThresholdExceeded(cnt) {
			log.Error("Rate limit #1 (" + appConfig.GetRateLimit1().String() + ") exceeded , count = " + strconv.Itoa(cnt))
			return true
		} else {
			log.Debug("Rate limit #1 (" + appConfig.GetRateLimit1().String() + ") NOT exceeded , count = " + strconv.Itoa(cnt))
		}
	} else {
		log.Debug("Rate limit #1 not configured")
	}
	if appConfig.GetRateLimit2() != nil {
		var cnt = c.countSms(appConfig.GetRateLimit2().Interval)
		if appConfig.GetRateLimit2().IsThresholdExceeded(cnt) {
			log.Error("Rate limit #2 (" + appConfig.GetRateLimit2().String() + ") exceeded , count = " + strconv.Itoa(cnt))
			return true
		} else {
			log.Debug("Rate limit #2 (" + appConfig.GetRateLimit2().String() + ") NOT exceeded , count = " + strconv.Itoa(cnt))
		}
	} else {
		log.Debug("Rate limit #2 not configured")
	}
	return false
}

func (c *State) WasSentAlready(msgId message.MessageId) bool {
	mutex.Lock()
	defer mutex.Unlock()

	for _, id := range c.data.PendingMessageIds {
		if id == msgId {
			return false
		}
	}
	return c.data.LastSuccessfulMessageId != nil && msgId.Compare(*c.data.LastSuccessfulMessageId) <= 0
}

// NewMessageId() obtains a new message ID and marks it as "pending"
func (c *State) NewMessageId() message.MessageId {
	mutex.Lock()
	defer mutex.Unlock()

	result := c.data.NextMessageId
	c.data.PendingMessageIds = append(c.data.PendingMessageIds, result)
	c.data.NextMessageId = c.data.NextMessageId.NextId()
	return result
}

// Deletes a pending message ID, returning TRUE if the deleted message ID
// was the oldest of all pending message IDs
func (c *State) deletePendingMessageId(msgId message.MessageId) {
	for idx, id := range c.data.PendingMessageIds {
		if id == msgId {
			c.data.PendingMessageIds = append(c.data.PendingMessageIds[:idx], c.data.PendingMessageIds[idx+1:]...)
			return
		}
	}
	return
}
func (c *State) DiscardMessageId(msgId message.MessageId) {
	mutex.Lock()
	defer mutex.Unlock()
	c.deletePendingMessageId(msgId)
}

func (c *State) RememberSmsSend(msg message.Message) {

	mutex.Lock()
	if c.data.LastSuccessfulMessageId != nil && msg.Id.Compare(*c.data.LastSuccessfulMessageId) <= 0 {
		mutex.Unlock()
		panic("RememberSmsSend() called with message ID " + msg.Id.String() + " that is equal to/older than last successful message ID " + c.data.LastSuccessfulMessageId.String())
	}

	c.deletePendingMessageId(msg.Id)
	c.data.LastSuccessfulMessageId = &msg.Id

	nowInSeconds := time.Now().Unix()
	c.data.Timestamps = append(c.data.Timestamps, UnixTimestamp(nowInSeconds))

	var cutOffTimestamp UnixTimestamp = -1
	if appConfig.GetRateLimit1() != nil {
		if appConfig.GetRateLimit2() != nil {
			if appConfig.GetRateLimit1().Interval.IsGreaterThan(&appConfig.GetRateLimit2().Interval) {
				cutOffTimestamp = UnixTimestamp(nowInSeconds - int64(appConfig.GetRateLimit1().Interval.ToSeconds()))
			} else {
				cutOffTimestamp = UnixTimestamp(nowInSeconds - int64(appConfig.GetRateLimit2().Interval.ToSeconds()))
			}
		} else {
			cutOffTimestamp = UnixTimestamp(nowInSeconds - int64(appConfig.GetRateLimit1().Interval.ToSeconds()))
		}
	} else if appConfig.GetRateLimit2() != nil {
		cutOffTimestamp = UnixTimestamp(nowInSeconds - int64(appConfig.GetRateLimit2().Interval.ToSeconds()))
	}

	if cutOffTimestamp != -1 {
		for i := len(c.data.Timestamps) - 1; i >= 0; i-- {
			if c.data.Timestamps[i] < cutOffTimestamp {
				var newLen = len(c.data.Timestamps) - (i + 1)
				c.data.Timestamps = c.data.Timestamps[:newLen]
				break
			}
		}
	}

	mutex.Unlock() // unlock before doing blocking I/O

	// WriteState() will log an error, we'll just swallow this one here
	// hoping that at some future time we'll be able to persist the state again....
	_ = c.WriteState()
}

func getStateFile() string {
	return appConfig.GetDataDirectory() + "/state.json"
}

func (c *State) WriteState() error {
	mutex.Lock()
	defer mutex.Unlock()
	return c.writeState()
}

func (c *State) writeState() error {

	jsonData, err := json.Marshal(c.data)
	if err != nil {
		panic("Error marshaling to JSON: %v" + err.Error())
	}

	log.Debug("Persisting application state to " + getStateFile())
	err = os.WriteFile(getStateFile(), jsonData, 0644)
	if err != nil {
		msg := "Failed to write state file '" + getStateFile() + "' - " + err.Error()
		log.Error(msg)
		return errors.New(msg)
	}
	return nil
}

func Init(config *config.Config) (*State, error) {

	mutex.Lock()
	defer mutex.Unlock()

	appConfig = *config

	result := &State{data: newInternalState()}
	if common.FileExist(getStateFile()) {
		content, err := os.ReadFile(getStateFile())
		if err != nil {
			return nil, errors.New("Failed to read state file: " + getStateFile() + " - " + err.Error())
		}
		err = json.Unmarshal(content, &result.data)
		if err != nil {
			return nil, errors.New("Failed to deserialize JSON state file: " + getStateFile() + " - " + err.Error())
		}
	} else {
		log.Info("State file '" + getStateFile() + "' does not exist, creating an empty file")
		err := result.writeState()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *State) GetLastSuccessfulSendTimestamp() *UnixTimestamp {

	if len(s.data.Timestamps) > 0 {
		cloned := s.data.Timestamps[len(s.data.Timestamps)-1]
		return &cloned
	}
	return nil
}

func (s *State) SetLastKeepAliveMessageEnqueued(ts UnixTimestamp) {

	mutex.Lock()
	defer mutex.Unlock()

	s.data.LastKeepAliveMsgEnqueued = &ts
}

func (s *State) GetLastKeepAliveMessageEnqueued() *UnixTimestamp {

	mutex.Lock()
	defer mutex.Unlock()

	if s.data.LastKeepAliveMsgEnqueued == nil {
		return nil
	}
	cloned := *s.data.LastKeepAliveMsgEnqueued
	return &cloned
}
