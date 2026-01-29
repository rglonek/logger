package logger

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"
)

var defaultLevel LogLevel = INFO

var defaultPrefix = ""

// rawTerminalMode is a global atomic flag indicating whether the terminal is in raw mode.
// When true, terminal output will use \r\n line endings instead of \n.
var rawTerminalMode atomic.Bool

// SetRawTerminalMode sets whether the terminal is in raw mode.
// When enabled, terminal output (stderr) will use \r\n line endings.
// Call with true when entering raw terminal mode (e.g., term.MakeRaw),
// and false when restoring normal terminal mode.
func SetRawTerminalMode(enabled bool) {
	rawTerminalMode.Store(enabled)
}

// GetRawTerminalMode returns whether raw terminal mode is currently enabled.
func GetRawTerminalMode() bool {
	return rawTerminalMode.Load()
}

// terminalWriter wraps an io.Writer and adjusts line endings based on raw terminal mode.
// When raw mode is enabled and the underlying writer is a terminal, \n is converted to \r\n.
type terminalWriter struct {
	w          io.Writer
	isTerminal bool
}

// newTerminalWriter creates a terminalWriter that wraps the given writer.
// It detects whether the writer is a terminal file descriptor using term.IsTerminal,
// which performs an ioctl check to verify it's a real terminal (not a pipe or file).
func newTerminalWriter(w io.Writer) *terminalWriter {
	isTerminal := false
	if f, ok := w.(*os.File); ok {
		isTerminal = term.IsTerminal(int(f.Fd()))
	}
	return &terminalWriter{w: w, isTerminal: isTerminal}
}

func (t *terminalWriter) Write(p []byte) (n int, err error) {
	if t.isTerminal && GetRawTerminalMode() {
		// Convert \n to \r\n for raw terminal mode
		// First normalize any existing \r\n to \n to avoid \r\r\n
		output := bytes.ReplaceAll(p, []byte("\r\n"), []byte("\n"))
		output = bytes.ReplaceAll(output, []byte("\n"), []byte("\r\n"))
		_, err = t.w.Write(output)
		return len(p), err // Return original length to satisfy io.Writer contract
	}
	return t.w.Write(p)
}

type LogLevel int

const (
	DETAIL   LogLevel = 6
	DEBUG    LogLevel = 5
	INFO     LogLevel = 4
	WARNING  LogLevel = 3
	ERROR    LogLevel = 2
	CRITICAL LogLevel = 1
)

type Logger struct {
	logLevel            LogLevel
	p                   string
	disableStderr       bool
	logToFile           string
	enableKmesg         bool
	fileLogger          *log.Logger
	stderrLogger        *log.Logger
	kmesg               *os.File
	milliseconds        bool
	sinkBufferLock      *sync.Mutex
	sinkBuffer          chan string
	sinkBufferTruncated *bool
	timeFormat          string
}

func (l *Logger) SinkBuffer(buffer chan string, truncated *bool) {
	l.sinkBuffer = buffer
	l.sinkBufferTruncated = truncated
	l.sinkBufferLock = new(sync.Mutex)
	l.timeFormat = "2006-01-02 15:04:05"
}

func (l *Logger) SinkDisableStderr() {
	l.disableStderr = true
}

func (l *Logger) SinkLogToFile(name string) (err error) {
	l.logToFile = name
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	l.fileLogger = log.New(f, "", log.Default().Flags())
	return nil
}

func (l *Logger) SinkEnableKmesg() error {
	l.enableKmesg = true
	kmsg, err := os.OpenFile("/dev/kmsg", os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	l.kmesg = kmsg
	return nil
}

var defaultLogger = &Logger{
	logLevel:     INFO, // 0=NO_LOGGING 1=CRITICAL, 2=ERROR, 3=WARNING, 4=INFO, 5=DEBUG, 6=DETAIL
	p:            "",
	stderrLogger: log.New(newTerminalWriter(os.Stderr), "", log.LstdFlags),
}

func Info(format string, v ...interface{}) {
	defaultLogger.Info(format, v...)
}

func Warn(format string, v ...interface{}) {
	defaultLogger.Warn(format, v...)
}

func Error(format string, v ...interface{}) {
	defaultLogger.Error(format, v...)
}

func Critical(format string, v ...interface{}) {
	defaultLogger.Critical(format, v...)
}

func Debug(format string, v ...interface{}) {
	defaultLogger.Debug(format, v...)
}

func Detail(format string, v ...interface{}) {
	defaultLogger.Detail(format, v...)
}

func SetPrefix(prefix string) {
	defaultLogger.SetPrefix(prefix)
	defaultPrefix = prefix
}

func SetLogLevel(level LogLevel) {
	defaultLogger.SetLogLevel(level)
	defaultLevel = level
}

func NewLogger() *Logger {
	return &Logger{
		logLevel:     defaultLevel,
		p:            defaultPrefix,
		stderrLogger: log.New(newTerminalWriter(os.Stderr), "", log.LstdFlags),
	}
}

func (l *Logger) MillisecondLogging(enable bool) {
	l.milliseconds = enable
	if enable {
		l.stderrLogger.SetFlags(log.LstdFlags | log.Lmicroseconds)
		if l.fileLogger != nil {
			l.fileLogger.SetFlags(log.LstdFlags | log.Lmicroseconds)
		}
		if l.sinkBuffer != nil {
			l.timeFormat = "2006-01-02 15:04:05.000"
		}
	} else {
		l.stderrLogger.SetFlags(log.LstdFlags)
		if l.fileLogger != nil {
			l.fileLogger.SetFlags(log.LstdFlags)
		}
		if l.sinkBuffer != nil {
			l.timeFormat = "2006-01-02 15:04:05"
		}
	}
}

func (l *Logger) WithPrefix(prefix string) *Logger {
	newLogger := &Logger{
		logLevel:            l.logLevel,
		p:                   fmt.Sprintf("%s%s", l.p, prefix),
		disableStderr:       l.disableStderr,
		logToFile:           l.logToFile,
		fileLogger:          l.fileLogger,
		kmesg:               l.kmesg,
		enableKmesg:         l.enableKmesg,
		stderrLogger:        l.stderrLogger,
		milliseconds:        l.milliseconds,
		sinkBuffer:          l.sinkBuffer,
		sinkBufferTruncated: l.sinkBufferTruncated,
		sinkBufferLock:      l.sinkBufferLock,
		timeFormat:          l.timeFormat,
	}
	return newLogger
}

func (l *Logger) WithLogLevel(level LogLevel) *Logger {
	newLogger := &Logger{
		logLevel:            level,
		p:                   l.p,
		disableStderr:       l.disableStderr,
		logToFile:           l.logToFile,
		fileLogger:          l.fileLogger,
		kmesg:               l.kmesg,
		enableKmesg:         l.enableKmesg,
		stderrLogger:        l.stderrLogger,
		milliseconds:        l.milliseconds,
		sinkBuffer:          l.sinkBuffer,
		sinkBufferTruncated: l.sinkBufferTruncated,
		sinkBufferLock:      l.sinkBufferLock,
		timeFormat:          l.timeFormat,
	}
	return newLogger
}

func (l *Logger) SetPrefix(prefix string) {
	l.p = prefix
}

func (l *Logger) SetLogLevel(level LogLevel) {
	if level < 0 {
		level = 0
	}
	l.logLevel = level
}

func (l *Logger) Info(format string, v ...interface{}) {
	if l.logLevel < 4 {
		return
	}
	format = "INFO " + l.p + format
	if !l.disableStderr {
		l.stderrLogger.Printf(format, v...)
	}
	if l.fileLogger != nil {
		l.fileLogger.Printf(format, v...)
	}
	if l.kmesg != nil {
		fmt.Fprintf(l.kmesg, "<5>"+format+"\n", v...)
	}
	if l.sinkBuffer != nil {
		l.sinkBufferLock.Lock()
		if len(l.sinkBuffer) >= cap(l.sinkBuffer) {
			<-l.sinkBuffer
			if l.sinkBufferTruncated != nil {
				*l.sinkBufferTruncated = true
			}
		}
		l.sinkBuffer <- time.Now().Format(l.timeFormat) + fmt.Sprintf(format, v...)
		l.sinkBufferLock.Unlock()
	}
}

func (l *Logger) Warn(format string, v ...interface{}) {
	if l.logLevel < 3 {
		return
	}
	format = "WARNING " + l.p + format
	if !l.disableStderr {
		l.stderrLogger.Printf(format, v...)
	}
	if l.fileLogger != nil {
		l.fileLogger.Printf(format, v...)
	}
	if l.kmesg != nil {
		fmt.Fprintf(l.kmesg, "<4>"+format+"\n", v...)
	}
	if l.sinkBuffer != nil {
		l.sinkBufferLock.Lock()
		if len(l.sinkBuffer) >= cap(l.sinkBuffer) {
			<-l.sinkBuffer
			if l.sinkBufferTruncated != nil {
				*l.sinkBufferTruncated = true
			}
		}
		l.sinkBuffer <- time.Now().Format(l.timeFormat) + fmt.Sprintf(format, v...)
		l.sinkBufferLock.Unlock()
	}
}

func (l *Logger) Error(format string, v ...interface{}) {
	if l.logLevel < 2 {
		return
	}
	format = "ERROR " + l.p + format
	if !l.disableStderr {
		l.stderrLogger.Printf(format, v...)
	}
	if l.fileLogger != nil {
		l.fileLogger.Printf(format, v...)
	}
	if l.kmesg != nil {
		fmt.Fprintf(l.kmesg, "<3>"+format+"\n", v...)
	}
	if l.sinkBuffer != nil {
		l.sinkBufferLock.Lock()
		if len(l.sinkBuffer) >= cap(l.sinkBuffer) {
			<-l.sinkBuffer
			if l.sinkBufferTruncated != nil {
				*l.sinkBufferTruncated = true
			}
		}
		l.sinkBuffer <- time.Now().Format(l.timeFormat) + fmt.Sprintf(format, v...)
		l.sinkBufferLock.Unlock()
	}
}

func (l *Logger) Critical(format string, v ...interface{}) {
	if l.logLevel >= 1 {
		format = "CRITICAL " + l.p + format
		if !l.disableStderr {
			l.stderrLogger.Printf(format, v...)
		}
		if l.fileLogger != nil {
			l.fileLogger.Printf(format, v...)
		}
		if l.kmesg != nil {
			fmt.Fprintf(l.kmesg, "<2>"+format+"\n", v...)
		}
		if l.sinkBuffer != nil {
			l.sinkBufferLock.Lock()
			if len(l.sinkBuffer) >= cap(l.sinkBuffer) {
				<-l.sinkBuffer
				if l.sinkBufferTruncated != nil {
					*l.sinkBufferTruncated = true
				}
			}
			l.sinkBuffer <- time.Now().Format(l.timeFormat) + fmt.Sprintf(format, v...)
			l.sinkBufferLock.Unlock()
		}
	}
	os.Exit(1)
}

func (l *Logger) Debug(format string, v ...interface{}) {
	if l.logLevel < 5 {
		return
	}
	format = "DEBUG " + l.p + format
	if !l.disableStderr {
		l.stderrLogger.Printf(format, v...)
	}
	if l.fileLogger != nil {
		l.fileLogger.Printf(format, v...)
	}
	if l.kmesg != nil {
		fmt.Fprintf(l.kmesg, "<6>"+format+"\n", v...)
	}
	if l.sinkBuffer != nil {
		l.sinkBufferLock.Lock()
		if len(l.sinkBuffer) >= cap(l.sinkBuffer) {
			<-l.sinkBuffer
			if l.sinkBufferTruncated != nil {
				*l.sinkBufferTruncated = true
			}
		}
		l.sinkBuffer <- time.Now().Format(l.timeFormat) + fmt.Sprintf(format, v...)
		l.sinkBufferLock.Unlock()
	}
}

func (l *Logger) Detail(format string, v ...interface{}) {
	if l.logLevel < 6 {
		return
	}
	format = "DETAIL " + l.p + format
	if !l.disableStderr {
		l.stderrLogger.Printf(format, v...)
	}
	if l.fileLogger != nil {
		l.fileLogger.Printf(format, v...)
	}
	if l.kmesg != nil {
		fmt.Fprintf(l.kmesg, "<7>"+format+"\n", v...)
	}
	if l.sinkBuffer != nil {
		l.sinkBufferLock.Lock()
		if len(l.sinkBuffer) >= cap(l.sinkBuffer) {
			<-l.sinkBuffer
			if l.sinkBufferTruncated != nil {
				*l.sinkBufferTruncated = true
			}
		}
		l.sinkBuffer <- time.Now().Format(l.timeFormat) + fmt.Sprintf(format, v...)
		l.sinkBufferLock.Unlock()
	}
}
