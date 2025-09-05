package main

import (
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/modem"
	"code-sourcery.de/sms-gateway/restapi"
	"code-sourcery.de/sms-gateway/state"
	"os"
	"os/signal"
	"syscall"
)

var log = logger.GetLogger("main")

func main() {
	log.Info("sms-gateway v1.0")

	if len(os.Args) != 2 {
		panic("Invalid command line - expected config file as only argument")
	}
	appConfig, err := config.LoadConfig(os.Args[1])
	if err != nil {
		panic(err)
	}
	log.Debug("Configuration loaded.")
	appState, err := state.Init(appConfig)
	if err != nil {
		panic(err)
	}
	defer func(appState *state.State) {
		_ = appState.WriteState()
	}(appState)

	err = modem.Init(appConfig, appState)
	if err != nil {
		panic(err)
	}
	defer modem.Shutdown()

	// modem.SendSms("test message")

	err = restapi.Init(appConfig)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = restapi.Shutdown()
	}()

	// Create a channel to receive signals.
	sigChan := make(chan os.Signal, 1)

	// Register the channel to be notified of SIGINT and SIGTERM.
	// SIGINT is for Ctrl+C, SIGTERM is for 'kill' or systemd stop.
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Info("Shutting down, received signal " + sig.String())
}
