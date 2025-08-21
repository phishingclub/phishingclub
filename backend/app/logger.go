package app

import (
	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/log"
	"github.com/phishingclub/phishingclub/version"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	MODE_INTEGRATION_TEST = "integration_test"
	MODE_DEVELOPMENT      = "development"
	MODE_PRODUCTION       = "production"
)

func createCore(core zapcore.Core) zapcore.Core {
	return &stackCore{core}
}

type stackCore struct {
	zapcore.Core
}

func (c *stackCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	// dont add our core again if it's already been added
	if ce != nil {
		return ce
	}
	return ce.AddCore(ent, c)
}

func (c *stackCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// return c.Core.Write(ent, fields)
	// look for error field and enhance the message with stack trace
	for _, field := range fields {
		if field.Key == "error" {
			if err, ok := field.Interface.(error); ok {
				if goErr, ok := err.(*errors.Error); ok {
					ent.Stack = goErr.ErrorStack()
				}
			}
		}
	}
	return c.Core.Write(ent, fields)
}

func SetupLogger(loggerType string, conf *config.Config) (*zap.SugaredLogger, *zap.AtomicLevel, error) {
	var logger *zap.Logger
	var loggerAtom *zap.AtomicLevel
	var err error

	switch loggerType {
	case MODE_DEVELOPMENT:
		logger, loggerAtom, err = log.NewDevelopmentLogger(conf)
	case MODE_INTEGRATION_TEST:
		fallthrough
	case MODE_PRODUCTION:
		fallthrough
	default:
		logger, loggerAtom, err = log.NewProductionLogger(conf)
	}

	if err != nil {
		return nil, nil, err
	}

	// Create new logger with custom core
	logger = zap.New(createCore(logger.Core()))
	sgr := logger.Sugar()
	if loggerType == MODE_PRODUCTION {
		sgr = sgr.With("v-debug", version.Get())
	}
	return sgr, loggerAtom, nil
}
