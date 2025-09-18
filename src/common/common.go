package common

import (
	"bufio"
	"code-sourcery.de/sms-gateway/logger"
	"errors"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

// this must be a VARIABLE and _NOT_ a constant so that "go build -ldflags="-X ..." can change this symbol's
// value in the executable
var APPLICATION_VERSION = "dev-snapshot"

var log = logger.GetLogger("common")

type Optional[T any] struct {
	value   T
	isEmpty bool
}

func NonEmptyOptional[T any](value T) Optional[T] {
	return Optional[T]{value: value, isEmpty: false}
}

func MapSlice[A any, B any](in []A, mapping func(A) B) []B {
	result := []B{}
	for _, value := range in {
		result = append(result, mapping(value))
	}
	return result
}

func EmptyOptional[T any]() Optional[T] {
	return Optional[T]{isEmpty: true}
}

func (opt Optional[T]) IsEmpty() bool {
	return opt.isEmpty
}

func (opt Optional[T]) Get() T {
	if opt.isEmpty {
		panic("get() invoked on EMPTY")
	}
	return opt.value
}

func (opt Optional[T]) IsPresent() bool {
	return !opt.isEmpty
}

type Comparator[T any] func(T, T) bool

type Predicate[T any] func(T) bool

type ListVisitor[T any] func(T, int) bool

type Iterator[T any] interface {
	HasNext() bool
	Next() T
	Remove()
}

type PointerIterator[T any] interface {
	HasNext() bool
	Next() *T
	Remove()
}

type Iterable[T any] interface {
	Iterator() Iterator[T]
}

type PointerIterable[T any] interface {
	Iterator() PointerIterator[T]
}

// Check whether a given filesystem path refers to a regular file.
func IsFile(path string) (bool, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return fileInfo.Mode().IsRegular(), nil
}

func AppendWithDeref[A any](destination []A, source []*A) []A {

	result := destination
	for _, a := range source {
		result = append(result, *a)
	}
	return result
}

// RemoveElementAtIndex removes a specific index from a slice.
func RemoveElementAtIndex[T any](s []*T, index int) []*T {
	if len(s) == 0 {
		return s
	}
	ret := make([]*T, len(s)-1)
	ret = append(ret, s[:index]...)
	return append(ret, s[index+1:]...)
}

func FileExist(file string) bool {
	return !FileDoesNotExist(file)
}

func FileIsSmallerThan(file string, offset int64) (bool, error) {
	stat, err := os.Stat(file)
	if err != nil {
		return false, err
	}
	return stat.Size() < offset, nil
}

func FileDoesNotExist(file string) bool {
	_, err := os.Stat(file)

	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		panic("Failed to check for file existence: " + err.Error())
	}
	return false
}

// helper functions to get keys from a golang map as a slice...
// no idea why this is not in the standard library yet...
func GetMapKeys[A comparable, B any](m map[A]B) []A {

	keys := make([]A, len(m))

	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	return keys
}

// ReadLines reads all lines from a file
func ReadLines(path string) ([]string, error) {

	file, err := os.Open(path)
	if err != nil {
		log.Error(err.Error())
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	result := make([]string, 0)
	for scanner.Scan() {
		result = append(result, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ReadFile loads a text file and returns its contents as bytes.
func ReadFile(path string) (*[]byte, error) {

	file, err := os.Open(path)

	if err != nil {
		log.Error("Failed to open file " + path + " for reading (" + err.Error() + ")")
		return nil, err
	}
	defer file.Close()

	byteResult, err := io.ReadAll(file)
	if err != nil {
		log.Error("Failed to read from file " + path + " (" + err.Error() + ")")
		return nil, err
	}
	return &byteResult, nil
}

// Write string to a file, overwriting any existing file at that location
func WriteFile(path string, fileContent []byte) error {

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		log.Error("Failed to open file " + path + " for writing (" + err.Error() + ")")
		return err
	}
	defer file.Close()

	_, err = file.Write(fileContent)
	if err != nil {
		log.Error("Failed to write to file " + path + " (" + err.Error() + ")")
		return err
	}
	return nil
}

func SleepMillis(millis uint32) {
	time.Sleep(time.Duration(millis) * time.Millisecond)
}

func SleepMillisInterruptible(millis uint32, cancelSleep *atomic.Bool) {

	remaining := millis
	for remaining > 0 && !cancelSleep.Load() {
		var timeToSleep uint32
		if remaining > 500 {
			timeToSleep = 500
		} else {
			timeToSleep = remaining
		}
		time.Sleep(time.Duration(timeToSleep) * time.Millisecond)
		remaining -= timeToSleep
	}
}

func RegisterShutdownHandler(handler func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			log.Info("Received signal " + sig.String())
			if sig == os.Interrupt || sig == os.Kill {
				handler()
				break
			}
		}
	}()
}

func GetNameFromFileName(fileName string) string {
	if fileName == "" {
		panic("Unreachable code reached")
	}
	for i := len(fileName) - 1; i > 0; i-- {
		if fileName[i] == '/' {
			return fileName[i+1:]
		}
	}
	return fileName
}

func GetDirectoryFromFileName(fileName string) string {
	if strings.Index(fileName, "/") != 0 {
		panic("File name " + fileName + " does not start with /")
	}

	lastIndex := -1
	for i := len(fileName) - 1; i > 0; i-- {
		if fileName[i] == '/' {
			lastIndex = i
			break
		}
	}
	if lastIndex == -1 {
		return "/"
	}
	return fileName[:lastIndex]
}

func IsBlank(s string) bool {
	if len(s) == 0 {
		return true
	}
	for _, c := range s {
		if !unicode.IsSpace(c) {
			return false
		}
	}
	return true
}

func TimeToString(t time.Time) string {
	format := "2006-01-02 15:04:05-0700"
	return t.Format(format)
}

func AToInt64(input string) (int64, error) {
	return strconv.ParseInt(input, 10, 64)
}
