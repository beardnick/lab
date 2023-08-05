package infra

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
)

var Logger zerolog.Logger

func init() {
	level := os.Getenv("LOGLEVEL")
	l := zerolog.InfoLevel
	switch level {
	case "debug":
		l = zerolog.DebugLevel
	case "error":
		l = zerolog.ErrorLevel
	case "fatal":
		l = zerolog.FatalLevel
	}
	Logger = log.With().Logger().Level(l)
}
