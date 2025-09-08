package modem

import (
	"strings"
	"testing"
)

func decode(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "<CR>", "\r"), "<LF>", "\n")
}
func encode(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", "<CR>"), "\n", "<LF>")
}

func runTest(input string, expected []string, requiresOkOrError bool, t *testing.T) {

	println("Testing: " + encode(input))

	testData := []byte(decode(input))
	currentIndex := 0

	var charProvider = func() CharResult {
		if currentIndex == len(testData) {
			return CharResult{char: 0x00, timeout: true, err: nil}
		}
		c := testData[currentIndex]
		currentIndex++
		return CharResult{char: c, timeout: false, err: nil}
	}
	lines, err := parseModemResponse(charProvider, requiresOkOrError)
	if err != nil {
		t.Errorf("failed to parse response: %s", err.Error())
	} else {
		if len(lines) != len(expected) {
			t.Errorf("wrong number of lines, expected %d, got %d", len(expected), len(lines))
		}
		for i, line := range lines {
			println("GOT: >" + line + "<")
			if line != expected[i] {
				t.Errorf("wrong line, expected : %s\n, got : %s", encode(expected[i]), encode(line))
			}
		}
	}
}

func TestParse(t *testing.T) {
	runTest("test\r\nOK\r\n", []string{"test", "OK"}, true, t)
	runTest("test", []string{"test"}, false, t)
	runTest("\r\n", []string{}, false, t)
	runTest("\r\n\r\n", []string{}, false, t)
	runTest("test\r\ntest", []string{"test", "test"}, false, t)
	runTest("test\r\ntest\r\n", []string{"test", "test"}, false, t)
	runTest("OK\r\ntest\r\n", []string{"OK"}, true, t)
	runTest("ERROR\r\ntest\r\n", []string{"ERROR"}, false, t)

	s := "<CR><LF>+CGDCONT: (1-11),\"IP\",,,(0-2),(0-3),(0,1),(0,1)<CR><LF>+CGDCONT: (1-11),\"PPP\",,,(0-2),(0-3),(0,1),(0,1)<CR><LF><CR><LF><CR><LF>OK<CR><LF>"
	runTest(s, []string{"+CGDCONT: (1-11),\"IP\",,,(0-2),(0-3),(0,1),(0,1)",
		"+CGDCONT: (1-11),\"PPP\",,,(0-2),(0-3),(0,1),(0,1)",
		"OK"}, true, t)
}
