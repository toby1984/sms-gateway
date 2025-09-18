package main

import (
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/keepalive"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/modem"
	"code-sourcery.de/sms-gateway/restapi"
	"code-sourcery.de/sms-gateway/state"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var log = logger.GetLogger("main")

var gitCommit string
var buildTimestamp string
var buildVersion string

func main() {
	log.Info("sms-gateway v" + buildVersion + " (" + buildTimestamp + " @ " + gitCommit + ")")

	configFile := ""
	sendTestSms := false

	for idx, arg := range os.Args {
		if idx > 0 {
			if arg == "-test" || arg == "--test" {
				sendTestSms = true
			} else {
				if strings.HasPrefix(arg, "-") {
					panic("Invalid command line - unknown option '" + arg + "'")
				}
				if configFile != "" {
					panic("Invalid command line - unknown extra argument '" + arg + "'")
				}
				configFile = arg
			}
		}
	}

	if configFile == "" {
		panic("Invalid command line - expected config file as only argument")
	}
	appConfig, err := config.LoadConfig(configFile)
	if err != nil {
		panic(err)
	}
	log.Debug("Configuration loaded.")

	appState, err := state.Init(appConfig)
	if err != nil {
		panic(err)
	}

	log.Debug("Loading application state...")
	defer func(appState *state.State) {
		_ = appState.WriteState()
	}(appState)

	log.Debug("Starting REST api....")
	err = restapi.Init(appConfig, appState)
	if err != nil {
		panic(err)
	}
	log.Debug("REST api started.")

	log.Debug("Starting keep-alive...")
	keepalive.Init(appConfig, appState)
	defer keepalive.Shutdown()
	log.Debug("Keep-alive started.")

	if sendTestSms {
		modem.SendSms("test SMS, please ignore")
		modem.Close()
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
