package modem

import (
	"regexp"
	"strings"
)

// regex matching a newline sent by the modem
var lineFeedRegEx = regexp.MustCompile("\r\n")

// CharResult represents a single byte received from the modem, an error if something
// went wrong during serial communication or a flag indicating no more bytes could be
// read within the configured serial timeout.
type CharResult struct {
	char    byte
	timeout bool
	err     error
}

func (c CharResult) String() string {
	return string(rune(c.char))
}

var _ = CharResult{}.String

type CharProvider func() CharResult

// An expectedString the parser is currently trying to match
type expectedString struct {
	// the expected byte sequence to match
	expected []byte
	// the character we're currently trying to match
	// (index into the expected byte sequence)
	currentIndex int
	// the next expected strings
	// or an empty slice if the match should be considered complete after this string has been recognized.
	next []*expectedString
}

/*
 * Byte sequences we're looking for
 */
var terminatingNewline = &expectedString{expected: []byte("\r\n"), currentIndex: 0, next: []*expectedString{}}
var okMsg = &expectedString{expected: []byte("OK"), currentIndex: 0, next: []*expectedString{terminatingNewline}}
var errorMsg = &expectedString{expected: []byte("ERROR"), currentIndex: 0, next: []*expectedString{terminatingNewline}}
var startingNewline = &expectedString{expected: []byte("\r\n"), currentIndex: 0, next: []*expectedString{okMsg, errorMsg}}

type responseMatcher struct {
	matched           []*expectedString
	currentlyMatching *expectedString
	buffer            strings.Builder
	lines             []string
}

func (r *responseMatcher) isEmpty() bool {
	return len(r.lines) == 0
}

func (r *responseMatcher) lastLine() string {
	return r.lines[len(r.lines)-1]
}

func (r *responseMatcher) resetStateMachine() {
	r.currentlyMatching = startingNewline
	r.matched = r.matched[:0]
}

func (r *responseMatcher) wasMatched(match *expectedString) bool {
	for _, elem := range r.matched {
		if elem == match {
			return true
		}
	}
	return false
}

type parseState int

const (
	PARSE_STATE_MATCH_DONE = iota
	PARSE_STATE_MATCH_FAILED
	PARSE_STATE_MATCH_CONTINUE
)

// returns TRUE if end of response has been recognized
func (r *responseMatcher) tryMatch(data byte) parseState {

	if r.currentlyMatching.currentIndex == len(r.currentlyMatching.expected) {
		// we're past the end of the string we're currently currentlyMatching.

		// check next candidates and pick the currentlyMatching one
		for _, candidate := range r.currentlyMatching.next {
			if candidate.expected[0] == data {
				// first char of candidate got matched
				r.currentlyMatching = candidate
				r.currentlyMatching.currentIndex = 1
				// if candidate matched only a single character and
				// has no follow-up candidate, persist the whole tryMatch
				if len(r.currentlyMatching.expected) == 1 && len(r.currentlyMatching.next) == 0 {
					return PARSE_STATE_MATCH_DONE
				}
				return PARSE_STATE_MATCH_CONTINUE
			}
		}
		// none of the candidates were matched
		return PARSE_STATE_MATCH_FAILED
	}

	if r.currentlyMatching.expected[r.currentlyMatching.currentIndex] == data {
		// new character matched what we were already currentlyMatching
		r.currentlyMatching.currentIndex++
		if r.currentlyMatching.currentIndex == len(r.currentlyMatching.expected) {
			// we've done currentlyMatching the current expectedString,
			// remember what we've matched
			r.matched = append(r.matched, r.currentlyMatching)
			if len(r.currentlyMatching.next) == 0 {
				// no more to tryMatch
				return PARSE_STATE_MATCH_DONE
			}
		}
		return PARSE_STATE_MATCH_CONTINUE
	}
	r.currentlyMatching = nil
	return PARSE_STATE_MATCH_FAILED
}

func (r *responseMatcher) flushCharBuffer() {
	if r.buffer.Len() > 0 {

		bufAsString := r.buffer.String()
		r.buffer.Reset()
		for _, line := range lineFeedRegEx.Split(bufAsString, -1) {
			if len(strings.TrimSpace(line)) > 0 {
				r.lines = append(r.lines, line)
			}
		}
	}
}

func parseModemResponse(byteFromModem CharProvider, requiresOkOrError bool) ([]string, error) {

	matcher := responseMatcher{}
	matcher.resetStateMachine()

loop:
	for {
		var nextChar = byteFromModem()
		if nextChar.err != nil {
			return nil, nextChar.err
		}
		if nextChar.timeout {
			if requiresOkOrError {
				log.Debug("Timeout but still expecting OK or ERROR, keep waiting for response")
				continue
			}
			break
		}
		var parserState = matcher.tryMatch(nextChar.char)
		switch parserState {
		case PARSE_STATE_MATCH_DONE:
			matcher.buffer.WriteByte(nextChar.char)
			definitiveResponseEndingDetected := matcher.wasMatched(okMsg) || matcher.wasMatched(errorMsg)
			if definitiveResponseEndingDetected {
				// we've reached the last line of the modem's response,
				// either <cr><lf>OK<cr><lf> or <cr><lf>ERROR<cr><lf>
				break loop
			}
			// not the last line of the modem's response yet
			matcher.flushCharBuffer()
			if !matcher.isEmpty() && strings.Contains(matcher.lastLine(), "ERROR") {
				break loop
			}
			matcher.resetStateMachine()
		case PARSE_STATE_MATCH_CONTINUE:
			if matcher.currentlyMatching == startingNewline && matcher.currentlyMatching.currentIndex == 1 {
				// we've just matched the first character of the starting newline, flush
				// any characters we've already accumulated before continuing
				matcher.flushCharBuffer()
			}
			matcher.buffer.WriteByte(nextChar.char)
		case PARSE_STATE_MATCH_FAILED:
			matcher.buffer.WriteByte(nextChar.char)
			matcher.resetStateMachine()
		}
	}
	matcher.flushCharBuffer()
	return matcher.lines, nil
}
