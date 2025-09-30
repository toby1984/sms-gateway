package msgqueue

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/deliveryfailure"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/message"
	"code-sourcery.de/sms-gateway/modem"
	"code-sourcery.de/sms-gateway/state"
)

var log = logger.GetLogger("msgqueue")

var dataDir string

var inboxDir string
var sentDir string

var appState *state.State
var appConfig *config.Config

var inboxWatcherMutex sync.Mutex
var shutdownTriggered atomic.Bool
var inboxWatcherRunning atomic.Bool
var inboxWatcherShutdownLatch sync.WaitGroup

func createDir(parentDir string, childName string) (string, error) {
	dataDir := parentDir
	if !strings.HasSuffix(dataDir, "/") {
		dataDir += "/"
	}
	dir := dataDir + childName
	if !common.FileExist(dir) {
		err := os.Mkdir(dir, 0755)
		if err != nil {
			log.Error("Failed to create directory '" + dir + "' - " + err.Error())
			return "", err
		}
	}
	return dir, nil
}

func listFilesInInbox() ([]string, error) {

	// Read the contents of the directory
	entries, err := os.ReadDir(inboxDir)
	if err != nil {
		log.Error("Failed to list files in inbox directory - " + err.Error())
		return nil, err
	}

	// Loop over the entries and print their names
	result := []string{}
	for _, entry := range entries {
		file := inboxDir + "/" + entry.Name()
		log.Trace("File in inbox: " + file)
		result = append(result, file)
	}
	return result, nil
}

// StoreMessage stores a message into the inbox directory, ready to be sent.
func StoreMessage(id message.MessageId, text string) error {

	creationTime := time.Now()
	msg := message.Message{Id: id, CreationTimestamp: creationTime}
	msg.AbsPath = inboxDir + "/" + msg.ToFileName()
	msg.FileName = msg.ToFileName()

	tmpPath := msg.AbsPath + ".tmp"
	log.Debug("Storing message " + id.String() + " to " + tmpPath)
	file, err := os.Create(tmpPath)
	if err != nil {
		return errors.New("Failed to create file " + msg.String() + " : " + err.Error())
	}
	bytesWritten, err := file.Write([]byte(text))
	if err != nil {
		return errors.New("Failed to write file " + msg.String() + " : " + err.Error())
	}
	if bytesWritten != len(text) {
		return errors.New("Failed to write " + strconv.Itoa(len(text)) + " bytes to file " + msg.String())
	}
	err = file.Close()
	if err != nil {
		return errors.New("Failed to close file " + tmpPath + " : " + err.Error())
	}

	log.Debug("Renaming " + tmpPath + " => " + msg.AbsPath)
	err = os.Rename(tmpPath, msg.AbsPath)
	if err != nil {
		return errors.New("Failed to rename file " + tmpPath + " -> " + msg.AbsPath + " : " + err.Error())
	}
	return nil
}

func inboxWatcher() {

	inboxWatcherMutex.Lock()
	inboxWatcherShutdownLatch.Add(1)
	inboxWatcherRunning.Store(true)
	defer inboxWatcherRunning.Store(false)
	defer inboxWatcherShutdownLatch.Done()
	inboxWatcherMutex.Unlock()

	log.Info("Starting to watch inbox")
	for !shutdownTriggered.Load() {
		log.Trace("Woke up, checking inbox...")
		files, err := listFilesInInbox()
		if err == nil {
			for _, absPath := range files {
				msg, err := message.MsgFromFileName(absPath)
				if err != nil {
					log.Warn("Ignoring absPath " + absPath + " with malformed name: " + err.Error())
				} else {

					if deliveryfailure.IsDue(msg.Id) {
						rateLimitExceeded, err := sendMessage(msg)
						if err != nil {
							if rateLimitExceeded && appConfig.IsDropOnRateLimit() {
								deliveryfailure.DeliveryAborted(msg.Id)
							} else {
								deliveryfailure.DeliveryFailed(msg.Id)
								log.Error("Failed to sent '" + absPath + "' - " + err.Error())
							}
						} else {
							deliveryfailure.DeliverySuccessful(msg.Id)
						}
					}
				}
			}
		}
		time.Sleep(1 * time.Second)
	}
	log.Info("Stopping to watch inbox")
}

func sendMessage(msg *message.Message) (bool, error) {

	if !appState.WasSentAlready(msg.Id) {
		rawBytes, err := common.ReadFile(msg.AbsPath)
		if err != nil {
			log.Error("Failed to read file '" + msg.AbsPath + "' - " + err.Error())
			return false, err
		}
		if len(*rawBytes) == 0 {
			log.Info("File " + msg.AbsPath + " has length of zero bytes, just deleting it.")
			err = os.Remove(msg.AbsPath)
			if err != nil {
				log.Warn("Failed to delete file '" + msg.AbsPath + "' - " + err.Error())
			}
			return false, nil
		}
		result := modem.SendSms(string(*rawBytes))
		if !result.Success {
			if result.Reason == modem.MODEM_ERR_RATE_LIMIT_EXCEEDED {
				if appConfig.IsDropOnRateLimit() {
					err = os.Remove(msg.AbsPath)
					if err != nil {
						log.Warn("Failed to delete file '" + msg.AbsPath + "' after rate limit got exceeded - " + err.Error())
					}
					log.Warn("DISCARDED message '" + msg.AbsPath + "' after rate limit got exceeded")
				}
				return true, errors.New("Rate limit exceeded")
			}
			return false, errors.New("Failed to send SMS: " + result.Reason.String() + ", details: " + result.Details)
		}
		log.Info("Message sent successfully: " + msg.String())

		appState.RememberSmsSend(*msg)
	}

	// move message to "sent" folder
	newFile := sentDir + "/" + msg.FileName
	err := os.Rename(msg.AbsPath, newFile)
	if err != nil {
		log.Error("Failed to rename file '" + msg.AbsPath + "' -> " + newFile + " : " + err.Error())
	} else {
		log.Debug("Moved file '" + msg.AbsPath + "' -> " + newFile)
	}
	return false, nil
}

func Init(c *config.Config, state *state.State) error {

	var err error
	err = modem.Init(c, state)
	if err != nil {
		return err
	}
	defer modem.Close()

	appState = state
	appConfig = c

	// create top-level directory
	dataDir, err = createDir(c.GetDataDirectory(), "messages")
	if err != nil {
		return err
	}

	// create inbox directory
	inboxDir, err = createDir(dataDir, "inbox")
	if err != nil {
		return err
	}

	// create sent directory
	sentDir, err = createDir(dataDir, "sent")
	if err != nil {
		return err
	}

	go inboxWatcher()
	return nil
}

func Shutdown() {

	log.Debug("Shutting down message queue")
	inboxWatcherMutex.Lock()
	shutdownTriggered.Store(true)
	waitForInboxWatcherToStop := inboxWatcherRunning.Load()
	inboxWatcherMutex.Unlock()

	if waitForInboxWatcherToStop {
		log.Debug("Waiting for inbox watcher to shutdown")
		inboxWatcherShutdownLatch.Wait()
	}
	log.Debug("Message queue shut down")
}
