package msgqueue

import (
	"errors"
	"regexp"
	"strconv"
	"time"
)

/*
 * Message ID
 */
type MessageId int64

func (id MessageId) String() string {
	return strconv.FormatInt(int64(id), 10)
}

func (id MessageId) IsOlder(other MessageId) bool {
	return id.Compare(other) < 0
}

func (id MessageId) IsNewer(other MessageId) bool {
	return id.Compare(other) > 0
}

func (id MessageId) Compare(other MessageId) int {
	if id < other {

	}
	if id > other {
		return 1
	}
	return 0
}

/*
 * Message
 */

type Message struct {
	id                MessageId
	file              string
	creationTimestamp time.Time
}

func (m *Message) ToFileName() string {
	return m.id.String() + "_" + strconv.FormatInt(m.creationTimestamp.Unix(), 10)
}

func MsgFromFileName(fileName string) (*Message, error) {
	re := regexp.MustCompile("^([0-9]+)_([0-9]+).sms")
	match := re.FindStringSubmatch(fileName)
	if match == nil || len(match) != 3 {
		return nil, errors.New("Not a valid message filename: " + fileName)
	}
	msgId, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return nil, errors.New("Not a valid message filename (msg ID out of range): " + fileName)
	}
	unixTimestamp, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return nil, errors.New("Not a valid message filename (creationTimestamp out of range): " + fileName)
	}
	return &Message{id: MessageId(msgId), creationTimestamp: time.Unix(unixTimestamp, 0), file: fileName}, nil
}

func (m *Message) String() string {
	return "msg_id. " + m.id.String() + ", creationTimestamp: " + m.creationTimestamp.String() + ", file: " + m.file
}
