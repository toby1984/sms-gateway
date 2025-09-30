package main

import (
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/keepalive"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/msgqueue"
	"code-sourcery.de/sms-gateway/restapi"
	"code-sourcery.de/sms-gateway/state"
)

var log = logger.GetLogger("main")

var gitCommit string
var buildTimestamp string
var buildVersion string

func main() {
	log.Info("sms-gateway v" + buildVersion + " (" + buildTimestamp + " @ " + gitCommit + ")")

	configFile := ""
	testSms := ""

	var debugFlags []config.DebugFlag

	for idx := 0; idx < len(os.Args); idx = idx + 1 {
		arg := os.Args[idx]
		if idx > 0 {
			if arg == "-h" || arg == "-help" || arg == "--help" {
				println("Usage: [-h|-help|--help] [-t|--test <message>] [-d|--debug <flags>] <CONFIG FILE>")
				println()
				println("-h | -help | --help => Print help")
				println("-t | --test => Send test SMS")
				println("<-d | --debug> <flags> => Set debug flags. Possible flags are: 'modem_always_fail', 'modem_always_succeed'")
				return
			} else if arg == "-d" || arg == "--debug" {

				if (idx + 1) >= len(os.Args) {
					panic("'" + arg + "' option requires an argument")
				}
				flags := strings.Split(os.Args[idx+1], ",")
				idx = idx + 1
				for _, flag := range flags {
					flagConstant, err := config.ParseDebugFlag(flag)
					if err != nil {
						panic("Unknown debug flag '" + flag + "'")
					}
					debugFlags = append(debugFlags, flagConstant)
				}
			} else if arg == "-t" || arg == "--test" {

				if (idx + 1) >= len(os.Args) {
					panic("'" + arg + "' option requires an argument")
				}
				testSms = os.Args[idx+1]
				idx = idx + 1
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
	appConfig, err := config.LoadConfig(configFile, true)
	if err != nil {
		panic(err)
	}

	log.Info("Configuration loaded, using log level " + appConfig.GetLogLevel().String())
	logger.SetLogLevel(appConfig.GetLogLevel())

	if len(debugFlags) > 0 {
		log.Info("Using debug flags: " + common.Join(debugFlags, ",", func(f config.DebugFlag) string { return "'" + f.String() + "'" }))
		appConfig.SetDebugFlags(debugFlags)
	}

	if appConfig.GetMaxMessageLength() > 0 {
		log.Info("Will truncate messages exceeding " + strconv.Itoa(appConfig.GetMaxMessageLength()) + " characters")
	}

	if config.StartWatching(configFile) == nil {
		defer config.StopWatching()
	}

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

	if len(testSms) > 0 {
		msgId := appState.NewMessageId()

		err := msgqueue.StoreMessage(msgId, testSms)
		if err != nil {
			panic("Failed to send test message?")
		}
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
