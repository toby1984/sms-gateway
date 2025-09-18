package deliveryfailure

import (
	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/message"
	"math"
	"strconv"
	"sync"
	"time"
)

type DeliveryFailure struct {
	Id                   message.MessageId
	FailureCount         int
	LastFailureTimestamp time.Time
}

var log = logger.GetLogger("deliveryfailure")
var failuresLock sync.Mutex
var failures = make(map[message.MessageId]*DeliveryFailure)

func IsDue(msgId message.MessageId) bool {
	failuresLock.Lock()
	defer failuresLock.Unlock()

	failure, exists := failures[msgId]
	if !exists {
		return true
	}
	cnt := failure.FailureCount
	if cnt > 10 {
		cnt = 10
	}
	delaySeconds := int(math.Pow(float64(cnt), 3))
	delay := time.Duration(delaySeconds) * time.Second
	dueDate := failure.LastFailureTimestamp.Add(delay)
	now := time.Now()
	isDue := dueDate.Before(now) || dueDate.Equal(now)
	log.Trace("Msg " + msgId.String() + " has " + strconv.Itoa(failure.FailureCount) + " delivery failures, " +
		strconv.Itoa(delaySeconds) + " seconds delay, latest delivery failure at " + common.TimeToString(failure.LastFailureTimestamp) +
		", due date is " + common.TimeToString(dueDate) + " => is_due: " + strconv.FormatBool(isDue))
	return isDue
}

func DeliveryFailed(msgId message.MessageId) {
	failuresLock.Lock()
	defer failuresLock.Unlock()

	failure, exists := failures[msgId]
	if !exists {
		failure = &DeliveryFailure{Id: msgId}
		failures[msgId] = failure
	}
	failure.FailureCount++
	failure.LastFailureTimestamp = time.Now()
	log.Debug("Msg " + msgId.String() + " now has " + strconv.Itoa(failure.FailureCount) + " delivery failures")
}

func DeliverySuccessful(id message.MessageId) {
	failuresLock.Lock()
	defer failuresLock.Unlock()
	delete(failures, id)
	log.Debug("Msg " + id.String() + " got delivered successfully")
}
