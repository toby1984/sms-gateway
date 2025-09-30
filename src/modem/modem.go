package modem

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"code-sourcery.de/sms-gateway/config"
	"code-sourcery.de/sms-gateway/logger"
	"code-sourcery.de/sms-gateway/state"
	"go.bug.st/serial"
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
	MODEM_ERR_NONE                FailureReason = iota // = success/no error
	MODEM_ERR_RATE_LIMIT_EXCEEDED                      // too many SMS send within the configure time interval
	MODEM_ERR_MODEM_ERROR                              // either serial port or modem failure
)

func (failure FailureReason) String() string {
	switch failure {
	case MODEM_ERR_NONE:
		return "MODEM_ERR_NONE"
	case MODEM_ERR_RATE_LIMIT_EXCEEDED:
		return "MODEM_ERR_RATE_LIMIT_EXCEEDED"
	case MODEM_ERR_MODEM_ERROR:
		return "MODEM_ERR_MODEM_ERROR"
	default:
		panic("Unhandled failure reason")
	}
}

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
	for _, line := range r.Lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return &trimmed
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
	return r.getLineByPrefix(linePrefix)
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
			log.Info("SIM card needs PIN")
			return MODEM_PIN_REQUIRED, nil
		}
		if strings.HasPrefix(*line, "SIM PUK") {
			log.Warn("SIM card needs PUK")
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
	log.Info("Successfully unlocked modem using PIN")
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
	res, err := internalSendBytes(bytes, requiresOkOrError)
	if err != nil {
		log.Error("Closing serial port due to error " + err.Error())
		internalClose()
	}
	return res, err
}

func internalSendBytes(bytes []byte, requiresOkOrError bool) ([]string, error) {

	err := (*serialPort).ResetInputBuffer()
	if err != nil {
		log.Error("failed to drain serial input buffer: " + err.Error())
		return []string{}, err
	}

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
			log.Debug("*** timeout ***")
			return CharResult{char: 0x00, timeout: true, err: nil}
		}
		if log.IsDebugEnabled() {
			hexStringLower := fmt.Sprintf("%x", receivedByte[0])
			log.Debug("Received character: " + string(rune(receivedByte[0])) + " (0x" + hexStringLower + ")")
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
	result := internalSendSms(message)
	if !result.Success {
		if result.Reason == MODEM_ERR_MODEM_ERROR {
			Close()
		}
	}
	return result
}

type ConnectionStatus int

const (
	CON_STATUS_NOT_REGISTERED_NOT_SEARCHING ConnectionStatus = iota
	CON_STATUS_REGISTERED_HOME
	CON_STATUS_NOT_REGISTERED_SEARCHING
	CON_STATUS_NOT_REGISTERED_DENIED
	CON_STATUS_UNKNOWN
	CON_STATUS_REGISTERED_ROAMING
)

func (s ConnectionStatus) String() string {
	switch s {
	case CON_STATUS_NOT_REGISTERED_NOT_SEARCHING:
		return "NOT_REGISTERED_NOT_SEARCHING"
	case CON_STATUS_REGISTERED_HOME:
		return "REGISTERED_HOME"
	case CON_STATUS_NOT_REGISTERED_SEARCHING:
		return "NOT_REGISTERED_SEARCHING"
	case CON_STATUS_NOT_REGISTERED_DENIED:
		return "NOT_REGISTERED_DENIED"
	case CON_STATUS_UNKNOWN:
		return "UNKNOWN"
	case CON_STATUS_REGISTERED_ROAMING:
		return "REGISTERED_ROAMING"
	}
	panic("Unhandled switch/case: " + strconv.Itoa(int(s)))
}

func GetConnectionStatus() (ConnectionStatus, error) {
	if appConfig.IsSet(config.DEBUG_FLAG_MODEM_ALWAYS_SUCCEED) {
		return CON_STATUS_REGISTERED_HOME, nil
	}
	if appConfig.IsSet(config.DEBUG_FLAG_MODEM_ALWAYS_FAIL) {
		return CON_STATUS_UNKNOWN, errors.New("Failed because of DEBUG_FLAG_MODEM_ALWAYS_FAIL flag")
	}
	if needsInit() {
		err := initModem()
		if err != nil {
			return CON_STATUS_UNKNOWN, err
		}
	}
	mutex.Lock()
	defer mutex.Unlock()

	err := unlockSim()
	if err != nil {
		return CON_STATUS_UNKNOWN, err
	}

	response, err := sendCmd("AT+CREG?", true)
	if err != nil {
		return CON_STATUS_UNKNOWN, err
	}
	/*
			 * +CREG: <stat>[,<lac>,<ci>,<AcT>]
			 *
			 * <stat> (Status): A numeric value indicating the current network registration status.
			 *
		     * 0: Not registered. The modem isn't currently searching for a new operator.
			 * 1: Registered to the home network.
			 * 2: Not registered, but the modem is searching for an operator to register to.
			 * 3: Registration denied.
			 * 4: Unknown. For example, out of coverage.
			 * 5: Registered and roaming.
	*/
	line := response.getLineByPrefix("+CREG:")
	log.Debug("Modem response to CREG?: " + response.String())
	if line == nil {
		msg := "Unrecognized modem response (1)"
		log.Error(msg)
		return CON_STATUS_UNKNOWN, errors.New(msg)
	}
	parts := strings.Split(strings.TrimSpace((*line)[6:]), ",")
	if len(parts) < 2 {
		msg := "Unrecognized modem response (2)"
		log.Error(msg)
		return CON_STATUS_UNKNOWN, errors.New(msg)
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		msg := "Unrecognized modem response (3)"
		log.Error(msg)
		return CON_STATUS_UNKNOWN, errors.New(msg)
	}
	log.Debug("Modem response code: " + strconv.Itoa(code))

	switch code {
	case 0:
		return CON_STATUS_NOT_REGISTERED_NOT_SEARCHING, nil
	case 1:
		return CON_STATUS_REGISTERED_HOME, nil
	case 2:
		return CON_STATUS_NOT_REGISTERED_SEARCHING, nil
	case 3:
		return CON_STATUS_NOT_REGISTERED_DENIED, nil
	case 4:
		return CON_STATUS_UNKNOWN, nil
	case 5:
		return CON_STATUS_REGISTERED_ROAMING, nil
	default:
		msg := "Modem returned unknown result code " + strconv.Itoa(code)
		log.Error(msg)
		return CON_STATUS_UNKNOWN, errors.New(msg)
	}
}

func internalSendSms(message string) SendResult {

	if appConfig.IsSet(config.DEBUG_FLAG_MODEM_ALWAYS_SUCCEED) {
		log.Warn("Not actually sending SMS, DEBUG_FLAG_MODEM_ALWAYS_SUCCEED is set")
		log.Warn("Message: >" + message + "<")
		return SendResult{true, MODEM_ERR_NONE, "fake success (debug mode)"}
	}

	if appConfig.IsSet(config.DEBUG_FLAG_MODEM_ALWAYS_FAIL) {
		log.Warn("Not actually sending SMS, DEBUG_FLAG_MODEM_ALWAYS_FAIL is set")
		log.Warn("Message: >" + message + "<")
		return SendResult{false, MODEM_ERR_MODEM_ERROR, "fake modem failure (debug mode)"}
	}

	if needsInit() {
		err := initModem()
		if err != nil {
			return SendResult{Success: false, Reason: MODEM_ERR_MODEM_ERROR, Details: err.Error()}
		}
	}

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
			log.Error("Rate limit exceeded (current recipient: " + recipient + ")")
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
	}
	return SendResult{true, MODEM_ERR_NONE, "success"}
}

func Init(config *config.Config, state *state.State) error {

	appConfig = config
	appState = state
	return initModem()
}

func initModem() error {

	if appConfig.IsSet(config.DEBUG_FLAG_MODEM_ALWAYS_FAIL) || appConfig.IsSet(config.DEBUG_FLAG_MODEM_ALWAYS_SUCCEED) {
		log.Warn("Not initializing modem because of DEBUG_FLAG_MODEM_ALWAYS_FAIL / DEBUG_FLAG_MODEM_ALWAYS_SUCCEED")
		return nil
	}

	mutex.Lock()
	defer mutex.Unlock()

	mode := &serial.Mode{
		BaudRate: appConfig.GetSerialSpeed(),
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}

	serialDevName, err := appConfig.GetSerialPort()
	log.Debug("Initializing modem on port " + serialDevName + ", baud rate " + strconv.Itoa(appConfig.GetSerialSpeed()))

	// Open the serial port
	port, err := serial.Open(serialDevName, mode)
	if err != nil {
		var msg = "failed to open serial port '" + serialDevName + "' - " + err.Error()
		log.Error(msg)
		return errors.New(msg)
	}
	err = port.SetReadTimeout(appConfig.GetSerialReadTimeout())
	if err != nil {
		var msg = "failed to set read timeout " + appConfig.GetSerialReadTimeout().String() + " on serial port '" + serialDevName + "' - " + err.Error()
		log.Error(msg)
		return errors.New(msg)
	}

	// need to already assign global variable here
	// as sendCmd() uses it
	serialPort = &port

	cleanUp := func() {
		_ = port.Close()
		serialPort = nil
	}

	for _, cmd := range appConfig.GetModemInitCmds() {
		log.Debug("Executing modem init cmd: '" + cmd + "'")
		resp, err := sendCmd(cmd, false)
		if err != nil {
			cleanUp()
			return err
		}
		if resp.isError() {
			cleanUp()
			return errors.New("Running modem initialization cmd " + cmd + " returned an error: " + resp.String())
		}
	}
	return nil
}

func needsInit() bool {
	mutex.Lock()
	defer mutex.Unlock()
	return serialPort == nil
}

func Close() {

	mutex.Lock()
	defer mutex.Unlock()
	internalClose()
}

func internalClose() {

	if serialPort != nil {
		log.Info("Closing serial port")
		_ = (*serialPort).Close()
		serialPort = nil
	}
}
