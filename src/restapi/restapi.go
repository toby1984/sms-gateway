package restapi

import (
	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/msgqueue"
	"code-sourcery.de/sms-gateway/state"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var log = logger.GetLogger("rest-api")

var appState *state.State

type SendSmsRequest struct {
	Message string `json:"message"`
}

var httpServer *http.Server

func sendSms(c *gin.Context) {

	log.Debug("Incoming HTTP request")
	var req SendSmsRequest
	if err := c.BindJSON(&req); err != nil {
		_ = c.AbortWithError(500, errors.New("Failed to bind request to object"))
		return
	}

	log.Info("Incoming HTTP request with message '" + req.Message + "'")
	if strings.TrimSpace(req.Message) == "" {
		_ = c.AbortWithError(500, errors.New("SMS text cannot be empty or blank "))
		return
	}

	msgId := appState.NewMessageId()

	err := msgqueue.StoreMessage(msgId, req.Message)
	if err != nil {
		appState.DiscardMessageId(msgId)
		_ = c.AbortWithError(500, errors.New("Failed to store message for sending: "+err.Error()))
		return
	}
}

func Shutdown() error {

	var result error
	if httpServer != nil {
		log.Debug("Shutting down http server")
		ctx, closeFunc := context.WithTimeout(context.Background(), time.Duration(2000)*time.Millisecond)
		defer closeFunc()
		result = httpServer.Shutdown(ctx)
	}
	msgqueue.Shutdown()
	return result
}

func Init(config *config.Config, state *state.State) error {

	appState = state

	err := msgqueue.Init(config, appState)
	if err != nil {
		panic(err)
	}

	host := config.GetBindIp()
	port := config.GetBindPort()
	log.Debug("REST API starting up on " + host + ":" + strconv.Itoa(port))

	runGinInReleaseMode := config.GetLogLevel() != logger.LEVEL_DEBUG && config.GetLogLevel() != logger.LEVEL_TRACE
	if runGinInReleaseMode {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create gin instance
	var router *gin.Engine
	if runGinInReleaseMode {
		// Note that because of shitty defaults in Gin (see https://github.com/gin-gonic/gin/issues/598)
		// the only way to suppress the annoying request logger output to StdOut
		// is to create a custom instance and register the "middleware" yourself
		router = gin.New()
		router.Use(gin.Recovery())
	} else {
		router = gin.Default()
	}

	accounts := gin.Accounts{}
	accounts[config.GetUserName()] = config.GetPassword()
	authorized := router.Group("/", gin.BasicAuth(accounts))

	authorized.POST("/sendsms", sendSms)

	httpServer = &http.Server{
		Addr:    host + ":" + strconv.Itoa(port),
		Handler: router,
	}

	// Launch http server in a separate goroutine
	var immediateError = atomic.Pointer[error]{}

	go func() {
		// the next line will never return until the server is finished
		// UNLESS it fails to bind to the desired socket ...
		var err error
		if config.GetTLSConfig() != nil {
			tConf := config.GetTLSConfig()
			log.Info("TLS enabled, using cert " + tConf.CertFilePath + " and private key " + tConf.PrivateKeyFilePath)
			err = httpServer.ListenAndServeTLS(tConf.CertFilePath, tConf.PrivateKeyFilePath)
		} else {
			log.Warn("TLS NOT enabled by configuration, running unencrypted")
			err = httpServer.ListenAndServe()
		}

		if err != nil {
			immediateError.Store(&err)
		}
	}()
	common.SleepMillis(1000)
	var value = immediateError.Load()
	if value != nil {
		return *value
	}
	return nil
}
