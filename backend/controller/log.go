package controller

import (
	"context"
	"time"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type SetLevelRequest struct {
	Level   string `json:"level"`
	DBLevel string `json:"dbLevel"`
}

type Log struct {
	Common
	OptionService *service.Option
	Database      *gorm.DB
	LoggerAtom    *zap.AtomicLevel
}

// Panic is a test utility
func (c *Log) Panic(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if session == nil {
		if ok := c.handleErrors(g, errors.New("no session")); !ok {
			return
		}
	}
	c.Deeper()
}

func (c *Log) Deeper() {
	panic("panic test")
}

// Slow is a test utility
func (c *Log) Slow(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if session == nil {
		if ok := c.handleErrors(g, errors.New("no session")); !ok {
			return
		}
	}
	c.Logger.Debugf("Slow request testing start")
	time.Sleep(10 * time.Second)
	c.Logger.Debugf("Slow request testing stop")
	c.Response.OK(g, gin.H{})
}

// GetLevel gets the log level
func (c *Log) GetLevel(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// get the log levels
	logLevelOption, err := c.OptionService.GetOption(
		g,
		session,
		data.OptionKeyLogLevel,
	)
	// handle errors
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	dbLogLevelOption, err := c.OptionService.GetOption(g, session, data.OptionKeyDBLogLevel)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{
		"level":   logLevelOption.Value,
		"dbLevel": dbLogLevelOption.Value,
	})
}

// SetLevel sets the log level
func (c *Log) SetLevel(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var request SetLevelRequest
	if ok := c.handleParseRequest(g, &request); !ok {
		return
	}
	if request.Level == "" && request.DBLevel == "" {
		c.Response.BadRequestMessage(g, "level or dbLevel is required")
		return
	}
	if request.DBLevel != "" {
		switch request.DBLevel {
		case "silent":
			c.Database.Logger = c.Database.Logger.LogMode(logger.Silent)
		case "info":
			c.Database.Logger = c.Database.Logger.LogMode(logger.Info)
		case "warn":
			c.Database.Logger = c.Database.Logger.LogMode(logger.Warn)
		case "error":
			c.Database.Logger = c.Database.Logger.LogMode(logger.Error)
		default:
			c.Logger.Debugw("invalid db log level",
				"level", request.DBLevel,
			)
			c.Response.BadRequestMessage(g, "unknown DB log level")
			return
		}
		// set db log level in database
		dbLevel := vo.NewOptionalString1MBMust(request.DBLevel)
		dbLogLevelOption := model.Option{
			Key:   *vo.NewString64Must(data.OptionKeyDBLogLevel),
			Value: *dbLevel,
		}
		err := c.persist(
			g,
			session,
			&dbLogLevelOption,
		)
		// handle response
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	if request.Level != "" {
		switch request.Level {
		case "debug":
			c.LoggerAtom.SetLevel(zap.DebugLevel)
		case "info":
			c.LoggerAtom.SetLevel(zap.InfoLevel)
		case "warn":
			c.LoggerAtom.SetLevel(zap.WarnLevel)
		case "error":
			c.LoggerAtom.SetLevel(zap.ErrorLevel)
		default:
			c.Logger.Debugw("invalid log level",
				"level", request.Level,
			)
			c.Response.BadRequestMessage(g, "Unknown log level")
			return
		}

		// set log level in in memory logger struct
		logLevel := model.Option{
			Key:   *vo.NewString64Must(data.OptionKeyLogLevel),
			Value: *vo.NewOptionalString1MBMust(request.Level),
		}
		err := c.persist(
			g,
			session,
			&logLevel,
		)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	c.Response.OK(g, nil)
}

// TestLog tests the log
// Sends a log message for each log level debug, info, warn, error
func (c *Log) TestLog(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// check permissions
	isAuthorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		handleServerError(g, c.Response, err)
		return
	}
	if !isAuthorized {
		// TODO audit log
		c.Response.Unauthorized(g)
		return
	}
	c.Logger.Debug("Log: DEBUG Test")
	c.Logger.Info("Log: INFO Test")
	c.Logger.Warn("Log: WARN Test")
	c.Logger.Error("Log: ERROR Test")
	c.Response.OK(g, nil)
}

// persit saves the log level
// TODO this has become empty and superflous
func (c *Log) persist(
	ctx context.Context,
	session *model.Session,
	logLevel *model.Option,
) error {
	return c.OptionService.SetOptionByKey(
		ctx,
		session,
		logLevel,
	)
}
