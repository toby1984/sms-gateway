package logger

import (
	"errors"
	"log"
	"os"
	"sync"
)

type LogLevel int

const LEVEL_TRACE LogLevel = 5
const LEVEL_DEBUG LogLevel = 4
const LEVEL_INFO LogLevel = 3
const LEVEL_WARN LogLevel = 2
const LEVEL_ERROR LogLevel = 1

var currentLogLevel = LEVEL_DEBUG

type Logger struct {
	name string
}

func (l *Logger) doLog(severity string, msg string) {
	log.Printf("%s - %s - %s\n", severity, l.name, msg)
	_ = os.Stdout.Sync()
	_ = os.Stderr.Sync()
}

func SetLogLevel(level LogLevel) {
	currentLogLevel = level
}

func StringToLevel(level string) (LogLevel, error) {
	var result LogLevel
	var e error = nil
	switch level {
	case "TRACE":
		result = LEVEL_TRACE
	case "DEBUG":
		result = LEVEL_DEBUG
	case "INFO":
		result = LEVEL_INFO
	case "WARN":
		result = LEVEL_WARN
	case "ERROR":
		result = LEVEL_ERROR
	default:
		e = errors.New("Invalid log level string: " + level)
	}
	return result, e
}

func GetLogLevel() LogLevel {
	return currentLogLevel
}

func (l *Logger) IsTraceEnabled() bool {
	return currentLogLevel >= LEVEL_TRACE
}

func (l *Logger) IsDebugEnabled() bool {
	return currentLogLevel >= LEVEL_DEBUG
}

func (l *Logger) IsInfoEnabled() bool {
	return currentLogLevel >= LEVEL_INFO
}

func (l *Logger) IsWarnEnabled() bool {
	return currentLogLevel >= LEVEL_WARN
}

func (l *Logger) IsErrorEnabled() bool {
	return currentLogLevel >= LEVEL_ERROR
}

func (l *Logger) Trace(msg string) {
	if l.IsTraceEnabled() {
		l.doLog("TRACE", msg)
	}
}

func (l *Logger) Debug(msg string) {
	if l.IsDebugEnabled() {
		l.doLog("DEBUG", msg)
	}
}

func (l *Logger) Info(msg string) {
	if l.IsInfoEnabled() {
		l.doLog("INFO", msg)
	}
}

func (l *Logger) Warn(msg string) {
	if l.IsWarnEnabled() {
		l.doLog("WARN", msg)
	}
}

func (l *Logger) Error(msg string) {
	if l.IsErrorEnabled() {
		l.doLog("ERROR", msg)
	}
}

type LogManager struct {
	loggers_mutex sync.Mutex
	loggers       map[string]*Logger
}

func newLogManager() *LogManager {
	return &LogManager{
		loggers_mutex: sync.Mutex{},
		loggers:       make(map[string]*Logger),
	}
}

var loggers = sync.OnceValue(func() *LogManager {
	return newLogManager()
})

func GetLogManager() *LogManager {
	return loggers()
}

func GetLogger(name string) *Logger {
	return GetLogManager().GetLogger(name)
}

func (l *LogManager) GetLogger(name string) *Logger {
	l.loggers_mutex.Lock()
	defer l.loggers_mutex.Unlock()

	existing, ok := l.loggers[name]
	if ok {
		return existing
	}
	existing = &Logger{name: name}
	l.loggers[name] = existing
	return existing
}
