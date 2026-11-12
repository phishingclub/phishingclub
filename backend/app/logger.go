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

// SetupLogger builds the main logger and the audit logger.
// The audit logger writes to the same output as the main logger but ignores
// the main log level, so changing the level never stops audit events.
// When an audit log path is configured, audit events are also written there.
func SetupLogger(loggerType string, conf *config.Config) (*zap.SugaredLogger, *zap.AtomicLevel, *zap.SugaredLogger, error) {
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
		return nil, nil, nil, err
	}

	// Create new logger with custom core
	mainCore := createCore(logger.Core())
	logger = zap.New(mainCore)

	var auditFile zapcore.WriteSyncer
	if p := conf.AuditLogPath; len(p) > 0 {
		// audit events already go to the log file, using the same file twice
		// would write every event twice
		if len(conf.LogPath) > 0 && log.SameFile(p, conf.LogPath) {
			return nil, nil, nil, errors.New("log auditPath must not be the same file as log path")
		}
		f, err := log.OpenAuditFile(p)
		if err != nil {
			return nil, nil, nil, errors.Errorf("failed to open audit log file: %w", err)
		}
		auditFile = f
	}
	auditLogger := log.NewAuditLogger(mainCore, auditFile)

	sgr := logger.Sugar()
	auditSgr := auditLogger.Sugar()
	if loggerType == MODE_PRODUCTION {
		sgr = sgr.With("v-debug", version.Get())
		auditSgr = auditSgr.With("v-debug", version.Get())
	}
	return sgr, loggerAtom, auditSgr, nil
}
