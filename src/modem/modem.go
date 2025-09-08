package modem

import (
	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/state"
	"errors"
	"go.bug.st/serial"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

/*
 *Huawei init according to modem manager:
 *
 *
 * AT^CURC=0
 *
 */

var log = logger.GetLogger("modem")

var appConfig *config.Config
var appState *state.State

var mutex sync.Mutex

type FailureReason int

const (
	MODEM_ERR_NONE FailureReason = iota
	MODEM_ERR_RATE_LIMIT_EXCEEDED
	MODEM_ERR_MODEM_ERROR
)

type SendResult struct {
	Success bool
	Reason  FailureReason
	Details string
}

type ModemPinState int

const (
	MODEM_PIN_NOT_REQUIRED ModemPinState = iota
	MODEM_PIN_REQUIRED
	MODEM_PIN_PUK_REQUIRED
	MODEM_PIN_SERIAL_ERROR
	MODEM_PIN_RESPONSE_NOT_RECOGNIZED
)

var serialPort *serial.Port

type ModemResponse struct {
	Lines []string
}

func (r *ModemResponse) String() string {
	var sb strings.Builder
	for i, line := range r.Lines {
		sb.WriteString(line)
		if i+1 < len(r.Lines) {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (r *ModemResponse) getLineByPrefix(prefix string) *string {
	for i, line := range r.Lines {
		if strings.HasPrefix(line, prefix) {
			return &r.Lines[i]
		}
	}
	return nil
}

func (r *ModemResponse) getResponseLineFor(fullAtCmd string) *string {

	// fullAtCmd: AT+stuff[?)....
	// response: AT+stuff:......
	// OR
	// +CME....

	re := regexp.MustCompile("^AT\\+([A-Z]+).*$")
	atCmd := re.FindStringSubmatch(fullAtCmd)
	if atCmd == nil || len(atCmd) != 2 {
		panic("getResponseLineFor() function does not know how to handle AT command " + fullAtCmd)
	}
	var linePrefix = "+" + atCmd[1]
	log.Debug("Looking for line with prefix '" + linePrefix + "'")
	result := r.getLineByPrefix(linePrefix)
	if result != nil {
		var trimmed = strings.TrimSpace(*result)
		result = &trimmed
	}
	return result
}

func (r *ModemResponse) isError() bool {
	return !r.isOK()
}

func (r *ModemResponse) isOK() bool {
	if r.Lines == nil || len(r.Lines) == 0 {
		return false
	}
	for _, line := range r.Lines {
		if line == "OK" {
			return true
		}
	}
	return false
}

func (r *ModemResponse) IsEmpty() bool {
	return r.Size() == 0
}

func (r *ModemResponse) Size() int {
	if r.Lines == nil || len(r.Lines) == 0 {
		return 0
	}
	return len(r.Lines)
}

func queryPinState() (ModemPinState, error) {

	log.Debug("Querying SIM card PIN state...")
	response, err := sendCmd("AT+CPIN?", false)
	if err != nil {
		return MODEM_PIN_SERIAL_ERROR, err
	}

	log.Debug("Querying SIM card PIN state yielded " + response.String())

	/*
		+CPIN: READY: The SIM card is ready for use, and no PIN is required. This means the card is either unlocked or has no PIN enabled.
		+CPIN: SIM PIN: The SIM card is inserted, but it is locked and requires the PIN to be entered. You must send the AT+CPIN="<pin>" command to unlock it before you can use the modem for data services.
		+CPIN: SIM PUK: The SIM card has been locked due to three consecutive incorrect PIN entries. It now requires the PIN Unblocking Key (PUK) to be entered.
		+CME ERROR: 10: This indicates that the SIM card is not inserted or is not detected by the modem.
	*/
	line := response.getResponseLineFor("AT+CPIN")

	if line != nil {
		log.Debug("CPIN response:" + *line)
		if strings.Contains(*line, "READY") {
			log.Debug("SIM card PIN is unlocked")
			return MODEM_PIN_NOT_REQUIRED, nil
		}
		if strings.Contains(*line, "SIM PIN") {
			log.Debug("SIM card needs PIN")
			return MODEM_PIN_REQUIRED, nil
		}
		if strings.HasPrefix(*line, "SIM PUK") {
			log.Debug("SIM card needs PUK")
			return MODEM_PIN_PUK_REQUIRED, nil
		}
	} else {
		log.Warn("Failed to find CPIN response, looking for +CME")
		line = response.getLineByPrefix("+CME ERROR")
		if line != nil {
			return MODEM_PIN_RESPONSE_NOT_RECOGNIZED, errors.New("Modem sent an error in reply to AT+CPIN?:  " + response.String())
		}
	}
	return MODEM_PIN_RESPONSE_NOT_RECOGNIZED, errors.New("Modem sent unexpected response to AT+CPIN?: " + response.String())
}

func sendPin(pin string) error {
	resp, err := sendCmd("AT+CPIN=\""+pin+"\"", true)
	if err != nil {
		return errors.New("Failed to send PIN to modem: " + err.Error())
	}
	if resp.isError() {
		return errors.New("Unlocking PIN returned error response: " + resp.String())
	}
	return nil
}

func unlockSim() error {

	pinState, err := queryPinState()
	if err != nil {
		return err
	}
	switch pinState {
	case MODEM_PIN_NOT_REQUIRED:
		return nil
	case MODEM_PIN_REQUIRED:
		return sendPin(appConfig.GetSimPin())
	case MODEM_PIN_PUK_REQUIRED:
		return errors.New("Modem requires PUK, please unlock SIM card manually using AT+CPIN=\"<pin>\"")
	case MODEM_PIN_SERIAL_ERROR:
		return errors.New("Unlocking SIM card failed due to a serial error")
	case MODEM_PIN_RESPONSE_NOT_RECOGNIZED:
		return errors.New("Not sure whether SIM card unlocking succeeded, unexpected response to AT command.")
	}
	return nil
}

func sendBytes(bytes []byte, requiresOkOrError bool) ([]string, error) {
	bytesWritten, err := (*serialPort).Write(bytes)
	if err != nil {
		log.Error("failed to write to serial port: " + err.Error())
		return []string{}, err
	}
	if bytesWritten != len(bytes) {
		log.Error("failed to write() to serial port: write came up short")
		return []string{}, err
	}
	err = (*serialPort).Drain()
	if err != nil {
		log.Error("failed to drain() serial port: " + err.Error())
		return []string{}, err
	}

	var readResult = func() CharResult {
		var receivedByte = make([]byte, 1)
		bytesRead, err := (*serialPort).Read(receivedByte)
		if err != nil {
			return CharResult{char: 0x00, timeout: false, err: err}
		}
		if bytesRead == 0 {
			return CharResult{char: 0x00, timeout: true, err: nil}
		}
		return CharResult{char: receivedByte[0], timeout: false, err: nil}
	}

	lines, err := parseModemResponse(readResult, requiresOkOrError)
	if err != nil {
		log.Error("failed to read() to serial port: " + err.Error())
		return []string{}, err
	}
	if log.IsDebugEnabled() {
		log.Debug("sendBytes(): Modem response:\n" + strings.Join(lines, "\n"))
	}
	return lines, nil
}

func sendCmd(cmd string, requiresOkOrError bool) (ModemResponse, error) {

	if serialPort == nil {
		panic("Serial port not open?")
	}

	if strings.TrimSpace(cmd) == "" {
		return ModemResponse{Lines: []string{}}, errors.New("Command string cannot be blank or empty")
	}
	log.Debug("Sending AT command: '" + cmd + "'")
	if cmd[len(cmd)-1] != '\r' {
		cmd = cmd + "\r"
	}

	lines, err := sendBytes([]byte(cmd), requiresOkOrError)
	if err != nil {
		return ModemResponse{Lines: []string{}}, errors.New("Failed to send bytes - " + err.Error())
	}
	return ModemResponse{Lines: lines}, nil
}

func switchToPlainText() error {
	// switch modem to plain-text mode
	// AT+CMGF=1
	resp, err := sendCmd("AT+CMGF=1", true)
	if err != nil {
		return err
	}
	if resp.isError() {
		return errors.New("Failed to switch modem to plain-text mode: " + resp.String())
	}
	return nil
}

func SendSms(message string) SendResult {

	mutex.Lock()
	defer mutex.Unlock()

	err := unlockSim()
	if err != nil {
		return SendResult{Success: false, Reason: MODEM_ERR_MODEM_ERROR, Details: err.Error()}
	}

	// switch modem to plain-text mode
	// so AT+CMGS works
	err = switchToPlainText()
	if err != nil {
		return SendResult{Success: false, Reason: MODEM_ERR_MODEM_ERROR, Details: err.Error()}
	}

	for _, recipient := range appConfig.GetSmsRecipients() {

		if appState.IsAnyRateLimitExceeded() {
			return SendResult{false, MODEM_ERR_RATE_LIMIT_EXCEEDED, "Rate limit exceeded"}
		}

		log.Info("Sending sms to " + recipient)

		response, err := sendCmd("AT+CMGS=\""+recipient+"\"", false)
		if err != nil {
			log.Error("Failed to send sms to " + recipient + ": " + err.Error())
			return SendResult{Success: false, Reason: MODEM_ERR_MODEM_ERROR, Details: err.Error()}
		}
		if response.IsEmpty() || response.Size() != 1 || response.Lines[0] != "> " {
			log.Error("Failed to send sms to " + recipient + ": Expected '>' but got '" + response.String() + "'")
			return SendResult{Success: false, Reason: MODEM_ERR_MODEM_ERROR, Details: "Unrecognized modem response, expected '>'"}
		}
		// send actual message
		log.Debug("Sending actual message: '" + message + "'")
		toSent := []byte(message)
		toSent = append(toSent, 0x1a) // message needs to be terminated with CTRL-Z (0x1a)
		responseLines, err := sendBytes(toSent, true)
		if err != nil {
			return SendResult{Success: false, Reason: MODEM_ERR_MODEM_ERROR, Details: err.Error()}
		}
		response = ModemResponse{Lines: responseLines}
		log.Debug("Modem response: '" + response.String() + "'")
		if !response.isOK() {
			return SendResult{Success: false, Reason: MODEM_ERR_MODEM_ERROR, Details: response.String()}
		}
		appState.RememberSmsSend()
	}
	return SendResult{true, MODEM_ERR_NONE, "success"}
}

func Init(config *config.Config, state *state.State) error {

	mutex.Lock()
	defer mutex.Unlock()

	appConfig = config
	appState = state

	mode := &serial.Mode{
		BaudRate: config.GetSerialSpeed(),
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}

	log.Debug("Initializing modem on port " + config.GetSerialPort() + ", baud rate " + strconv.Itoa(config.GetSerialSpeed()))

	// Open the serial port
	port, err := serial.Open(config.GetSerialPort(), mode)
	if err != nil {
		var msg = "failed to open serial port '" + config.GetSerialPort() + "' - " + err.Error()
		log.Error(msg)
		return errors.New(msg)
	}
	err = port.SetReadTimeout(config.GetSerialReadTimeout())
	if err != nil {
		var msg = "failed to set read timeout " + config.GetSerialReadTimeout().String() + " on serial port '" + config.GetSerialPort() + "' - " + err.Error()
		log.Error(msg)
		return errors.New(msg)
	}
	serialPort = &port
	return nil
}

func Shutdown() {
	log.Debug("Shutting down modem")
	mutex.Lock()
	defer mutex.Unlock()

	if serialPort == nil {
		_ = (*serialPort).Close()
		serialPort = nil
	}
}
