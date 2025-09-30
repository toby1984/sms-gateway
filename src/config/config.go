package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code-sourcery.de/sms-gateway/common"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/serialportdiscovery"
	"code-sourcery.de/sms-gateway/util"
	"gopkg.in/ini.v1"
)

//go:embed default-config.conf
var DEFAULT_CONFIG []byte

// values are used in a BITMASK so must be power-of-two
const (
	DEBUG_FLAG_NONE                 DebugFlag = 0
	DEBUG_FLAG_MODEM_ALWAYS_SUCCEED DebugFlag = 1
	DEBUG_FLAG_MODEM_ALWAYS_FAIL              = 2
)

type DebugFlag int32

func parseDebugFlag(s string) (DebugFlag, error) {

	trimmed := strings.TrimSpace(s)
	switch trimmed {
	case "":
		return DEBUG_FLAG_NONE, nil
	case "modem_always_succeed":
		return DEBUG_FLAG_MODEM_ALWAYS_SUCCEED, nil
	case "modem_always_fail":
		return DEBUG_FLAG_MODEM_ALWAYS_FAIL, nil
	}
	return DEBUG_FLAG_NONE, errors.New("Unknown debug flag: " + s)
}

func stringToDebugFlags(s string) (int32, error) {
	result := int32(0)

	for _, token := range strings.Split(s, ",") {
		flag, err := parseDebugFlag(token)
		if err != nil {
			return 0, err
		}
		result = result | int32(flag)
	}
	return result, nil
}

type TlsConfig struct {
	CertFilePath       string
	PrivateKeyFilePath string
}

type Config struct {
	// common
	dataDirectory string
	logLevel      logger.LogLevel
	debugFlags    int32
	// REST API
	restUser     string
	restPassword string
	restPort     int
	bindIp       string
	// SIM
	simPin            string
	smsRecipients     []string
	rateLimit1        *util.RateLimit
	rateLimit2        *util.RateLimit
	keepAliveInterval *util.TimeInterval
	keepAliveMessage  string
	dropOnRateLimit   bool
	// modem
	modemInitCmds []string
	// serial
	usbDeviceId       *common.UsbDeviceId
	serialPort        string
	serialSpeed       int
	serialReadTimeout time.Duration
}

var log = logger.GetLogger("config")

func parseRateLimit(rateLimit string) (*util.RateLimit, error) {

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
	interval, err := parseTimeInterval(match[2])
	if err != nil {
		return nil, errors.New("Invalid rate limit string (time Interval): '" + rateLimit + "'")
	}
	return &util.RateLimit{Interval: *interval, Threshold: threshold}, nil
}

func parseTimeInterval(interval string) (*util.TimeInterval, error) {
	re := regexp.MustCompile(`^(\d+)([smhdw])`)
	match := re.FindStringSubmatch(interval)
	if match == nil || len(match) != 3 {
		return nil, errors.New("Invalid time Interval string: '" + interval + "'")
	}
	valueStr, err := strconv.Atoi(match[1])
	if err != nil {
		return nil, errors.New("Invalid time Interval string: '" + interval + "'")
	}
	unitStr, err := util.StringToTimeUnit(match[2])
	return &util.TimeInterval{valueStr, unitStr}, err
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

// ParseHex16Bit parses assumes an unsigned hexadecimal 16-bit number as an input string (like 'beef' or '12ab')
// and turns it into an uint16.
func ParseHex16Bit(hexString string) (uint16, error) {
	parsedInt64, err := strconv.ParseInt(hexString, 16, 16)
	if err != nil {
		fmt.Println("Error parsing hexadecimal string '"+hexString+"'", err)
		return 0, err
	}
	if parsedInt64 < 0 || parsedInt64 > 65535 {
		fmt.Println("Hexadecimal value '"+hexString+"' must be within 16-bit unsigned range", err)
		return 0, err
	}
	return uint16(parsedInt64), nil
}

func LoadConfig(path string, createIfMissing bool) (*Config, error) {

	var result Config
	var convError error

	if !common.FileExist(path) {
		if !createIfMissing {
			return nil, errors.New("Config file does not exist: " + path)
		}
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

	// [common] dataDirectory
	result.dataDirectory = cfg.Section("common").Key("dataDirectory").String()
	if strings.TrimSpace(result.dataDirectory) == "" {
		return fail("Invalid configuration value for key 'dataDirectory' in [common] section - value cannot be empty/blank/missing")
	}

	// [common] debugFlags
	key := cfg.Section("common").Key("debugFlags")
	if key != nil {
		result.debugFlags, convError = stringToDebugFlags(key.String())
		if convError != nil {
			return fail("Invalid configuration value for key 'debugFlags' in [common] section " + convError.Error())
		}
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

	// [sms] dropOnRateLimit
	s := cfg.Section("sms").Key("dropOnRateLimit").MustString("")
	if s == "" {
		result.dropOnRateLimit = false
	} else {
		result.dropOnRateLimit, convError = stringToBool(s)
		if convError != nil {
			return fail("Invalid configuration boolean value for key 'dropOnRateLimit' in [sms] section " + convError.Error())
		}
		if result.dropOnRateLimit {
			log.Warn("Will DROP any SMS exceeding the rate limit instead of queueing them.")
		}
	}
	// [sms] rateLimit1
	result.rateLimit1, convError = parseRateLimit(cfg.Section("sms").Key("rateLimit1").String())
	if convError != nil {
		return fail("Invalid configuration value for key 'rateLimit1' in [sms] section " + convError.Error())
	}
	if result.rateLimit1 != nil {
		log.Info("Rate limit #1: " + result.rateLimit1.String())
	} else {
		log.Info("Rate limit #1 not configured")
	}

	// [sms] rateLimit2
	result.rateLimit2, convError = parseRateLimit(cfg.Section("sms").Key("rateLimit2").String())
	if convError != nil {
		return fail("Invalid configuration value for key 'rateLimit2' in [sms] section " + convError.Error())
	}
	if result.rateLimit1 != nil {
		log.Info("Rate limit #2: " + result.rateLimit2.String())
	} else {
		log.Info("Rate limit #2 not configured")
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

	// [sms] keepAliveInterval
	iv := cfg.Section("sms").Key("keepAliveInterval").String()
	if iv != "" || strings.TrimSpace(iv) != "" {
		keepAlive, convError := parseTimeInterval(iv)
		if convError != nil {
			return fail("Invalid configuration value for key 'keepAliveInterval' in [sms] section - " + convError.Error())
		}
		result.keepAliveInterval = keepAlive
		result.keepAliveMessage = cfg.Section("sms").Key("keepAliveMessage").String()
		if strings.TrimSpace(result.keepAliveMessage) == "" {
			return fail("Invalid/missing configuration value for key 'keepAliveMessage' in [sms] section - a value is required if 'keepAliveInterval' is set")
		}
	}

	// [modem] usbDeviceId
	usbVendorId := cfg.Section("modem").Key("usbVendorId").MustString("")
	usbProductId := cfg.Section("modem").Key("usbProductId").MustString("")
	if usbVendorId != "" || usbProductId != "" {
		if usbVendorId == "" || usbProductId == "" {
			return fail("Either none or both of [modem] usbVendorId and usbProductId need to be specified")
		}
		vendorId, convError := ParseHex16Bit(usbVendorId)
		if convError != nil {
			return fail("Invalid configuration value for key 'usbVendorId' in [modem] section - " + convError.Error())
		}
		productId, convError := ParseHex16Bit(usbProductId)
		if convError != nil {
			return fail("Invalid configuration value for key 'usbProductId' in [modem] section - " + convError.Error())
		}
		result.usbDeviceId = &common.UsbDeviceId{VendorId: vendorId, ProductId: productId}
	} else {
		result.usbDeviceId = nil
	}

	// [modem] serialPort
	result.serialPort = strings.TrimSpace(cfg.Section("modem").Key("serialPort").String())
	if result.serialPort == "" {
		return fail("A value for [modem] serialPort is required")
	}
	if result.usbDeviceId != nil {
		val, convError := strconv.Atoi(result.serialPort)
		if convError != nil || val < 0 {
			return fail("When [modem] usbVendorId/usbProductId is configured, [modem] serialPort has to be a positive integer number.")
		}
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

func (c Config) GetRateLimit1() *util.RateLimit {
	return c.rateLimit1
}

func (c Config) GetRateLimit2() *util.RateLimit {
	return c.rateLimit2
}

func (c Config) GetSerialSpeed() int {
	return c.serialSpeed
}

func (c Config) GetSerialPort() (string, error) {

	if c.GetUsbDeviceId() != nil {
		iFaces, err := serialportdiscovery.DiscoverUsbInterfaces(*c.usbDeviceId)
		if err != nil {
			return "", err
		}
		if len(iFaces) == 0 {
			return "", errors.New("serial-port auto discovery found no usb interfaces")
		}

		idx, _ := strconv.Atoi(c.serialPort)
		if len(iFaces) <= idx {
			return "", errors.New("serial-port auto discovery found only " + strconv.Itoa(len(iFaces)) + " interfaces but" +
				"[modem] serialPort config requested interface #" + strconv.Itoa(idx))
		}
		discovered := iFaces[idx]
		log.Info("Going to use device #" + strconv.Itoa(idx) + " [" + discovered + "]")
		return discovered, nil
	}
	return c.serialPort, nil
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

func (c Config) GetKeepAliveInterval() *util.TimeInterval {
	if c.keepAliveInterval == nil {
		return nil
	}
	clone := *c.keepAliveInterval
	return &clone
}

func (c Config) GetKeepAliveMessage() string {
	return c.keepAliveMessage
}

func (c Config) IsSet(flag DebugFlag) bool {
	return (c.debugFlags & int32(flag)) != 0
}
func (c Config) IsNotSet(flag DebugFlag) bool {
	return !c.IsSet(flag)
}

func (c Config) GetUsbDeviceId() *common.UsbDeviceId {
	return c.usbDeviceId
}

func (c Config) IsDropOnRateLimit() bool {
	return c.dropOnRateLimit
}
