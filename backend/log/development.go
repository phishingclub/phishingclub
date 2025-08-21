package log

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TODO add a build tag to this

// log is the global development logger for the application
// it is always in debug mode and should only be used for
// poor mans debugging and not committed when used
var Log *zap.SugaredLogger

func init() {
	// NewDevelopmentLogger factory for *zap.Logger with development settings
	atom := zap.NewAtomicLevelAt(zap.DebugLevel)
	c := zap.Config{
		Level:            atom,
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
	c.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger, _ := c.Build()
	Log = logger.Sugar()
}

// Debugf logs a message at debug level
func Bug(args ...any) {
	// for each arg do a printf with %v
	for _, arg := range args {
		Log.Debugf("-->( %++v )", arg)
	}
}

func Stop(args ...any) {
	Bug(args...)
	panic(0)
}
