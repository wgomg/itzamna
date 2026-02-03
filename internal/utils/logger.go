package utils

import (
	"io"
	"log"
	"os"
	"strings"
)

type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelError LogLevel = "error"
)

type Logger struct {
	level       LogLevel
	infoLogger  *log.Logger
	errorLogger *log.Logger
	debugLogger *log.Logger
	fatalLogger *log.Logger
	RawBodyLog  bool
}

func NewLogger(level string, rawBodyLog bool) *Logger {
	logLevel := parseLogLevel(level)

	return &Logger{
		level:       logLevel,
		infoLogger:  log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile),
		errorLogger: log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
		debugLogger: log.New(
			os.Stdout,
			"DEBUG: ",
			log.Ldate|log.Ltime|log.Lshortfile,
		),
		fatalLogger: log.New(os.Stderr, "FATAL: ", log.Ldate|log.Ltime|log.Lshortfile),
		RawBodyLog:  rawBodyLog,
	}
}

func NewDiscardLogger() *Logger {
	return &Logger{
		level:       LevelInfo,
		infoLogger:  log.New(io.Discard, "", 0),
		errorLogger: log.New(io.Discard, "", 0),
		debugLogger: log.New(io.Discard, "", 0),
		fatalLogger: log.New(io.Discard, "", 0),
	}
}

func parseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l *Logger) Info(format string, v ...any) {
	if l.level == LevelError {
		return
	}
	l.infoLogger.Printf(format, v...)
}

func (l *Logger) Error(format string, v ...any) {
	l.errorLogger.Printf(format, v...)
}

func (l *Logger) Debug(format string, v ...any) {
	if l.level != LevelDebug {
		return
	}
	l.debugLogger.Printf(format, v...)
}

func (l *Logger) Fatal(v ...any) {
	l.fatalLogger.Fatal(v...)
}
