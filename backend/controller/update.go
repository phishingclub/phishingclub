package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/service"
)

type Update struct {
	Common
	UpdateService *service.Update
	OptionService *service.Option
}

// CheckForUpdateCached checks if there is a new update from cache
func (u *Update) CheckForUpdateCached(g *gin.Context) {
	session, _, ok := u.handleSession(g)
	if !ok {
		return
	}
	updateAvailable, usingSystemd, err := u.UpdateService.CheckForUpdateCached(g, session)
	if ok := u.handleErrors(g, err); !ok {
		return
	}
	u.Response.OK(g, gin.H{
		"updateAvailable": updateAvailable,
		"updateInApp":     usingSystemd,
	})
}

// CheckForUpdate checks if there is a new update
func (u *Update) CheckForUpdate(g *gin.Context) {
	session, _, ok := u.handleSession(g)
	if !ok {
		return
	}
	updateAvailable, usingSystemd, err := u.UpdateService.CheckForUpdate(g, session)
	if ok := u.handleErrors(g, err); !ok {
		return
	}
	u.Response.OK(g, gin.H{
		"updateAvailable": updateAvailable,
		"updateInApp":     usingSystemd,
	})
}

// GetUpdateDetails gets details about the newest software update
func (u *Update) GetUpdateDetails(g *gin.Context) {
	session, _, ok := u.handleSession(g)
	if !ok {
		return
	}
	opt, err := u.OptionService.GetOption(g, session, data.OptionKeyUsingSystemd)
	if ok := u.handleErrors(g, err); !ok {
		return
	}
	details, err := u.UpdateService.GetUpdateDetails(g, session)
	if err != nil && !errors.Is(err, errs.ErrNoUpdateAvailable) {
		if ok := u.handleErrors(g, err); !ok {
			return
		}
	}
	if errors.Is(err, errs.ErrNoUpdateAvailable) {
		u.Response.OK(g, gin.H{
			"updateAvailable": false,
			"updateInApp":     opt.Value.String() == data.OptionValueUsingSystemdYes,
			"downloadURL":     "",
			"latestVersion":   "",
		})
		return
	}
	u.Response.OK(g, gin.H{
		"updateAvailable": true,
		"updateInApp":     opt.Value.String() == data.OptionValueUsingSystemdYes,
		"downloadURL":     details.DownloadURL,
		"latestVersion":   details.LatestVersion,
	})
}

// RunUpdate performs an update
func (u *Update) RunUpdate(g *gin.Context) {
	session, _, ok := u.handleSession(g)
	if !ok {
		return
	}
	err := u.UpdateService.RunUpdate(g, session)
	if ok := u.handleErrors(g, err); !ok {
		return
	}
	u.Response.OK(g, gin.H{})
}
