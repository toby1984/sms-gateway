package config

import (
	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/logger"
	_ "embed"
	"errors"
	"gopkg.in/ini.v1"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed default-config.conf
var DEFAULT_CONFIG []byte

type TlsConfig struct {
	CertFilePath       string
	PrivateKeyFilePath string
}

type TimeInterval struct {
	value int
	unit  TimeUnit
}

type RateLimit struct {
	Threshold int
	Interval  TimeInterval
}

func (r *RateLimit) IsThresholdExceeded(value int) bool {
	return value > r.Threshold
}

type Config struct {
	// common
	dataDirectory     string
	logLevel          logger.LogLevel
	debugDisableModem bool
	// REST API
	restUser     string
	restPassword string
	restPort     int
	bindIp       string
	// SIM
	simPin        string
	smsRecipients []string
	rateLimit1    *RateLimit
	rateLimit2    *RateLimit
	// modem
	modemInitCmds []string
	// serial
	serialPort        string
	serialSpeed       int
	serialReadTimeout time.Duration
}

var log = logger.GetLogger("config")

type TimeUnit int

const (
	Seconds TimeUnit = iota
	Minutes
	Hours
	Days
	Weeks
)

func (d TimeUnit) String() string {
	switch d {
	case Seconds:
		return "Second"
	case Minutes:
		return "Minute"
	case Hours:
		return "Hour"
	case Days:
		return "Day"
	case Weeks:
		return "Week"
	default:
		return "Unknown"
	}
}

func StringToTimeUnit(unit string) (TimeUnit, error) {
	switch unit {
	case "s":
		return Seconds, nil
	case "m":
		return Minutes, nil
	case "h":
		return Hours, nil
	case "d":
		return Days, nil
	case "w":
		return Weeks, nil
	default:
		return 0, errors.New("Invalid time unit: '" + unit + "'")
	}
}

func ParseRateLimit(rateLimit string) (*RateLimit, error) {

	if rateLimit == "" || strings.TrimSpace(rateLimit) == "" {
		return nil, nil
	}

	re := regexp.MustCompile(`^(\d+)/(\d+[smhdw])`)
	match := re.FindStringSubmatch(rateLimit)
	if match == nil || len(match) != 3 {
		return nil, errors.New("Invalid rate limit string: '" + rateLimit + "'")
	}
	threshold, err := strconv.Atoi(match[1])
	if err != nil {
		return nil, errors.New("Invalid rate limit string (Threshold): '" + rateLimit + "'")
	}
	interval, err := ParseTimeInterval(match[2])
	if err != nil {
		return nil, errors.New("Invalid rate limit string (time Interval): '" + rateLimit + "'")
	}
	return &RateLimit{Interval: *interval, Threshold: threshold}, nil
}

func ParseTimeInterval(interval string) (*TimeInterval, error) {
	re := regexp.MustCompile(`^(\d+)([smhdw])`)
	match := re.FindStringSubmatch(interval)
	if match == nil || len(match) != 3 {
		return nil, errors.New("Invalid time Interval string: '" + interval + "'")
	}
	valueStr, err := strconv.Atoi(match[1])
	if err != nil {
		return nil, errors.New("Invalid time Interval string: '" + interval + "'")
	}
	unitStr, err := StringToTimeUnit(match[2])
	return &TimeInterval{valueStr, unitStr}, err
}

func (iv *TimeInterval) IsGreaterThan(other *TimeInterval) bool {
	return iv.Compare(other) > 0
}

func (iv *TimeInterval) IsLessThan(other *TimeInterval) bool {
	return iv.Compare(other) < 0
}

func (iv *TimeInterval) Equals(other *TimeInterval) bool {
	return iv.Compare(other) == 0
}

func (iv *TimeInterval) Compare(other *TimeInterval) int {
	a := iv.ToSeconds()
	b := other.ToSeconds()
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

func (iv *TimeInterval) Value() int {
	return iv.value
}

func (iv *TimeInterval) Unit() TimeUnit {
	return iv.unit
}

func (iv *TimeInterval) ToSeconds() int {
	factor := 1
	switch iv.unit {
	case Seconds:
		factor = 1
	case Minutes:
		factor = 60
	case Hours:
		factor = 60 * 60
	case Days:
		factor = 60 * 60 * 24
	case Weeks:
		factor = 60 * 60 * 24 * 7
	default:
		panic("Internal error, unhandled switch/case: " + iv.unit.String())
	}
	return iv.value * factor
}

func fail(msg string) (*Config, error) {
	log.Error(msg)
	return nil, errors.New(msg)
}

func stringToBool(s string) (bool, error) {
	lower := strings.ToLower(s)
	switch lower {
	case "1":
		return true, nil
	case "0":
		return false, nil
	case "y":
		return true, nil
	case "n":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "yes":
		return true, nil
	case "no":
		return false, nil
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, errors.New("Unrecognized boolean value '" + s + "', valid choices are '1', '0', 'y', 'n', 'yes', 'no', 'on', 'off'")
	}
}

func LoadConfig(path string) (*Config, error) {

	var result Config
	var convError error

	if !common.FileExist(path) {
		err := os.WriteFile(path, DEFAULT_CONFIG, 0600)
		if err != nil {
			return fail("Config file " + path + " does not exist and creating a default file failed with error " + err.Error())
		}
		return fail("Config file " + path + " does not exist, creating a default file you need to customize.")
	}
	cfg, err := ini.Load(path)
	if err != nil {
		return fail("Failed to load config file" + err.Error())
	}

	// [common] logLevel
	logLvl := cfg.Section("common").Key("logLevel").MustString("INFO")
	result.logLevel, convError = logger.StringToLevel(logLvl)
	if convError != nil {
		return fail("Invalid configuration value for key 'logLevel' in [common] section " + convError.Error())
	}
	logger.SetLogLevel(result.logLevel)

	// [common] dataDirectory
	result.dataDirectory = cfg.Section("common").Key("dataDirectory").String()
	if strings.TrimSpace(result.dataDirectory) == "" {
		return fail("Invalid configuration value for key 'dataDirectory' in [common] section - value cannot be empty/blank/missing")
	}

	// [common] debugDisableModem
	result.debugDisableModem, convError = stringToBool(cfg.Section("common").Key("debugDisableModem").String())
	if convError != nil {
		return fail("Invalid configuration value for key 'debugDisableModem' in [common] section " + convError.Error())
	}

	// [restapi] bindIp
	result.bindIp = cfg.Section("restapi").Key("bindIp").String()
	if strings.TrimSpace(result.bindIp) == "" {
		return fail("Invalid configuration value for key 'bindIp' in [restapi] section - value cannot be empty/missing/blank")
	}

	// [restapi] user
	result.restUser = cfg.Section("restapi").Key("user").String()
	if strings.TrimSpace(result.restUser) == "" {
		return fail("Invalid configuration value for key 'user' in [restapi] section - value cannot be empty/missing/blank")
	}

	// [restapi] password
	result.restPassword = cfg.Section("restapi").Key("password").String()
	if strings.TrimSpace(result.restPassword) == "" {
		return fail("Invalid configuration value for key 'password' in [restapi] section - value cannot be empty/missing/blank")
	}

	// [restapi] port
	result.restPort, convError = cfg.Section("restapi").Key("port").Int()
	if convError != nil {
		return fail("Invalid configuration value for key 'port' in [restapi] section " + convError.Error())
	}

	// [sms] rateLimit1
	result.rateLimit1, convError = ParseRateLimit(cfg.Section("[sms]").Key("rateLimit1").String())
	if convError != nil {
		return fail("Invalid configuration value for key 'rateLimit1' in [sms] section " + convError.Error())
	}

	// [sms] rateLimit2
	result.rateLimit2, convError = ParseRateLimit(cfg.Section("[sms]").Key("rateLimit2").String())
	if convError != nil {
		return fail("Invalid configuration value for key 'rateLimit2' in [sms] section " + convError.Error())
	}

	// [sms] recipients
	recipients := cfg.Section("sms").Key("recipients").String()
	if recipients == "" || strings.TrimSpace(recipients) == "" {
		return fail("Invalid configuration value for key 'recipients' in [sms] section - value cannot be empty/blank/missing")
	}
	parts := strings.Split(recipients, ",")
	for _, recipient := range parts {
		result.smsRecipients = append(result.smsRecipients, strings.TrimSpace(recipient))
	}

	// [modem] serialPort
	result.serialPort = cfg.Section("modem").Key("serialPort").String()
	if strings.TrimSpace(result.serialPort) == "" {
		return fail("Invalid configuration value for key 'serialPort' in [modem] section - value cannot be empty/blank/missing")
	}

	// [modem] serialSpeed
	result.serialSpeed, convError = cfg.Section("modem").Key("serialSpeed").Int()
	if convError != nil {
		return fail("Invalid configuration value for key 'serialSpeed' in [modem] section " + convError.Error())
	}

	// [modem] serialReadTimeoutSeconds
	readTimeoutSeconds, convError := cfg.Section("modem").Key("serialReadTimeoutSeconds").Int()
	if convError != nil {
		return fail("Invalid configuration value for key 'serialReadTimeoutSeconds' in [modem] section " + convError.Error())
	}
	result.serialReadTimeout = time.Duration(readTimeoutSeconds) * time.Second

	// [modem] simPin
	result.simPin = cfg.Section("modem").Key("simPin").String()
	if strings.TrimSpace(result.simPin) == "" {
		return fail("Invalid configuration value for key 'simPin' in [modem] section - value cannot be empty/blank/missing")
	}

	// [modem] initCmds
	initCmds := cfg.Section("modem").Key("initCmds").String()
	result.modemInitCmds = strings.Split(initCmds, "\\r")
	return &result, nil
}

func (c Config) GetBindIp() string {
	return c.bindIp
}

func (c Config) GetBindPort() int {
	return c.restPort
}

func (c Config) GetUserName() string {
	return c.restUser
}

func (c Config) GetPassword() string {
	return c.restPassword
}

func (c Config) GetTLSConfig() *TlsConfig {
	// FIXME: Add TLS support
	return nil
}

func (c Config) GetRateLimit1() *RateLimit {
	return c.rateLimit1
}

func (c Config) GetRateLimit2() *RateLimit {
	return c.rateLimit2
}

func (c Config) GetSerialSpeed() int {
	return c.serialSpeed
}

func (c Config) GetSerialPort() string {
	return c.serialPort
}

func (c Config) GetSerialReadTimeout() time.Duration {
	return c.serialReadTimeout
}

func (c Config) GetDataDirectory() string {
	return c.dataDirectory
}

func (c Config) GetSimPin() string {
	return c.simPin
}

func (c Config) GetSmsRecipients() []string {
	return c.smsRecipients
}

func (c Config) GetLogLevel() logger.LogLevel {
	return c.logLevel
}

func (c Config) GetModemInitCmds() []string {
	return c.modemInitCmds
}

func (c Config) IsDebugDisableModem() bool {
	return c.debugDisableModem
}
