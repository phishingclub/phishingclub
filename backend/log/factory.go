package log

import (
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/errs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewDevelopmentLogger factory for *zap.Logger with development settings
func NewIntegrationTestLogger() (*zap.Logger, error) {
	c := zap.NewProductionConfig()
	c.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	return c.Build()
}

// NewDevelopmentLogger factory for *zap.Logger with development settings
func NewDevelopmentLogger(conf *config.Config) (*zap.Logger, *zap.AtomicLevel, error) {
	atom := zap.NewAtomicLevelAt(zap.DebugLevel)
	outPath := []string{"stderr"}
	errorOutPath := []string{"stderr"}
	if p := conf.LogPath; len(p) > 0 {
		outPath = append(outPath, p)
	}
	if p := conf.ErrLogPath; len(p) > 0 {
		errorOutPath = append(errorOutPath, p)
	}
	c := zap.Config{
		Level:            atom,
		Development:      true,
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      outPath,
		ErrorOutputPaths: errorOutPath,
	}
	c.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger, err := c.Build()

	return logger, &atom, errs.Wrap(err)
}

// NewProductionLogger factory for *zap.Logger with production settings
func NewProductionLogger(conf *config.Config) (*zap.Logger, *zap.AtomicLevel, error) {
	atom := zap.NewAtomicLevelAt(zap.InfoLevel)

	outPath := []string{"stderr"}
	errorOutPath := []string{"stderr"}
	if p := conf.LogPath; len(p) > 0 {
		outPath = append(outPath, p)
	}
	if p := conf.ErrLogPath; len(p) > 0 {
		errorOutPath = append(errorOutPath, p)
	}
	c := zap.Config{
		Level:            atom,
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      outPath,
		ErrorOutputPaths: errorOutPath,
	}
	logger, err := c.Build()
	return logger, &atom, errs.Wrap(err)
}
