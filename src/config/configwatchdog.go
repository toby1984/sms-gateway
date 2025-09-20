package config

import (
	"code-sourcery.de/sms-gateway/logger"
	"github.com/fsnotify/fsnotify"
	"path/filepath"
	"sync"
)

var fileModificationCount int64 = 0
var mu sync.Mutex
var fileChangedCond = sync.NewCond(&mu)

var watcher *fsnotify.Watcher = nil

func StopWatching() {
	if watcher != nil {
		log.Info("Stopping config file watcher")
		_ = watcher.Close()
	}
}

func watchFile(filePath string) {
	log.Info("Watching for changes on " + filePath)
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				log.Info("Config file watcher got closed")
				return
			}
			log.Trace("event:" + event.String())
			isWriteOrCreate := event.Has(fsnotify.Write) || event.Has(fsnotify.Create)
			if isWriteOrCreate && filepath.Base(event.Name) == filepath.Base(filePath) {
				log.Debug("config file modified:" + event.String())
				mu.Lock()
				fileModificationCount++
				mu.Unlock()
				fileChangedCond.Signal()
			}
		case err, _ := <-watcher.Errors:
			if err != nil {
				log.Error("Config file watcher failed:" + err.Error())
			}
		}
	}
}

func StartWatching(filePath string) error {

	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Error("Failed to create watcher: " + err.Error())
		return err
	}
	err = watcher.Add(filepath.Dir(filePath))
	if err != nil {
		_ = watcher.Close()
		watcher = nil
		log.Error("Failed to add file " + filepath.Dir(filePath) + " to watcher: " + err.Error())
		return err
	}
	go watchFile(filePath)
	go func() {

		mu.Lock()
		currentModificationCount := fileModificationCount
		mu.Unlock()
		for {
			mu.Lock()
			for fileModificationCount == currentModificationCount {
				fileChangedCond.Wait()
			}
			currentModificationCount = fileModificationCount
			mu.Unlock()

			log.Info("Reloading configuration from " + filePath)
			newConfig, err := LoadConfig(filePath, false)
			if err == nil {
				currentLvl := logger.GetLogLevel()
				newLvl := newConfig.GetLogLevel()
				log.Debug("Current log level: " + currentLvl.String() + " | new log level: " + newLvl.String())
				if newLvl != currentLvl {
					log.Info("Log level change detected: " + currentLvl.String() + " -> " + newLvl.String())
					logger.SetLogLevel(newLvl)
				} else {
					log.Trace("Log level stays the same: " + newLvl.String())
				}
			} else {
				log.Debug("Configuration reload failed: " + err.Error())
			}
		}
	}()
	return nil
}
