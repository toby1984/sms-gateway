package keepalive

import (
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/msgqueue"
	"code-sourcery.de/sms-gateway/state"
	"sync"
	"sync/atomic"
	"time"
)

var log = logger.GetLogger("keepalive")

var initialized atomic.Bool
var threadLock sync.Mutex
var threadRunning atomic.Bool
var shutdown atomic.Bool

var threadAlive sync.WaitGroup
var shutdownLatch sync.WaitGroup

var appState *state.State
var appConfig *config.Config

func keepAliveThread() {
	threadRunning.Store(true)
	threadAlive.Done()

	defer func() {
		shutdownLatch.Done()
		log.Info("Keep-alive thread terminated.")
		threadRunning.Store(false)
	}()

	log.Info("Keep-alive thread started")

	for !shutdown.Load() {
		ts1 := appState.GetLastSuccessfulSendTimestamp()
		ts2 := appState.GetLastKeepAliveMessageEnqueued()

		var latestTimestamp *state.UnixTimestamp
		if ts1 != nil && ts2 != nil {
			if ts1.ToTime().After(ts2.ToTime()) {
				latestTimestamp = ts1
			} else {
				latestTimestamp = ts2
			}
		} else if ts1 != nil {
			latestTimestamp = ts1
		} else if ts2 != nil {
			latestTimestamp = ts2
		}
		if latestTimestamp != nil {
			timeSinceLastMessage := time.Now().Sub(latestTimestamp.ToTime())
			sendKeepAlive := appConfig.GetKeepAliveInterval().IsShorterThan(timeSinceLastMessage)
			if sendKeepAlive {
				log.Debug("Scheduling keep-alive message")
				msgId := appState.NewMessageId()
				err := msgqueue.StoreMessage(msgId, appConfig.GetKeepAliveMessage())
				if err == nil {
					log.Info("Successfully scheduled keep-alive message")
					appState.SetLastKeepAliveMessageEnqueued(state.UnixTimestamp(time.Now().Unix()))
					_ = appState.WriteState()
				} else {
					log.Error("Failed to schedule keep-alive message: " + err.Error())
				}
			}
		} else {
			log.Debug("Keep-alive not active yet, no messages were ever send")
		}
		time.Sleep(1 * time.Second)
	}
	log.Info("Keep-alive thread was asked to shut down")
}

func Init(config *config.Config, state *state.State) {

	appState = state
	appConfig = config

	if config.GetKeepAliveInterval() == nil {
		log.Info("No keep-alive interval configured, won't start thread.")
		return
	}

	threadLock.Lock()
	defer threadLock.Unlock()

	if !initialized.CompareAndSwap(false, true) {
		panic("Already initialized")
	}
	shutdownLatch.Add(1)
	threadAlive.Add(1)
	go keepAliveThread()
	threadAlive.Wait()
}

func Shutdown() {
	threadLock.Lock()
	defer threadLock.Unlock()
	shutdown.Store(true)
	if threadRunning.Load() {
		shutdownLatch.Wait()
	}
}
