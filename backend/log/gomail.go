package log

import (
	"fmt"

	"github.com/wneessen/go-mail/log"
	"go.uber.org/zap"
)

// GoMailLoggerAdapter adapts zap.SugaredLogger to go-mail's Logger interface
type GoMailLoggerAdapter struct {
	logger *zap.SugaredLogger
}

func (z *GoMailLoggerAdapter) formatLogMessage(l log.Log) string {
	direction := "CLIENT"
	if l.Direction == log.DirServerToClient {
		direction = "SERVER"
	}

	// format the message with arguments if present
	msg := l.Format
	if len(l.Messages) > 0 {
		// create interface slice for fmt.Sprintf
		args := make([]interface{}, len(l.Messages))
		for i, m := range l.Messages {
			args[i] = m
		}
		msg = fmt.Sprintf(l.Format, args...)
	}

	return fmt.Sprintf("SMTP %s: %s", direction, msg)
}

func (z *GoMailLoggerAdapter) Debugf(l log.Log) {
	z.logger.Debug(z.formatLogMessage(l))
}

func (z *GoMailLoggerAdapter) Infof(l log.Log) {
	z.logger.Info(z.formatLogMessage(l))
}

func (z *GoMailLoggerAdapter) Warnf(l log.Log) {
	z.logger.Warn(z.formatLogMessage(l))
}

func (z *GoMailLoggerAdapter) Errorf(l log.Log) {
	z.logger.Error(z.formatLogMessage(l))
}

// NewGoMailLoggerAdapter creates a new go-mail logger adapter
func NewGoMailLoggerAdapter(logger *zap.SugaredLogger) *GoMailLoggerAdapter {
	return &GoMailLoggerAdapter{logger: logger}
}
