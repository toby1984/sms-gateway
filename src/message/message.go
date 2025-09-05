package message

import (
	"errors"
	"path/filepath"
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

func (id MessageId) NextId() MessageId {
	return id + 1
}

func FirstMessageId() MessageId {
	return 1
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
	Id                MessageId
	AbsPath           string
	FileName          string
	CreationTimestamp time.Time
}

func (m *Message) ToFileName() string {
	return m.Id.String() + "_" + strconv.FormatInt(m.CreationTimestamp.Unix(), 10)
}

func MsgFromFileName(fullPath string) (*Message, error) {
	fileName := filepath.Base(fullPath)
	re := regexp.MustCompile("^([0-9]+)_([0-9]+)")
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
		return nil, errors.New("Not a valid message filename (CreationTimestamp out of range): " + fileName)
	}
	return &Message{Id: MessageId(msgId), CreationTimestamp: time.Unix(unixTimestamp, 0),
		AbsPath: fullPath, FileName: fileName}, nil
}

func (m *Message) String() string {
	return "msg_id. " + m.Id.String() + ", CreationTimestamp: " + m.CreationTimestamp.String() + ", File: " + m.AbsPath
}
