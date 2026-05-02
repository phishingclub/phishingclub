package model

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// RemoteBrowserConfig is the API-level config for a remote browser session.
type RemoteBrowserConfig struct {
	Mode       string   `json:"mode"`       // "local" | "remote"
	Remote     string   `json:"remote"`     // DevTools WS URL (mode=remote only)
	Proxy      string   `json:"proxy"`      // socks5:// or http://
	Headless   bool     `json:"headless"`   // run Chrome headless (mode=local)
	Timeout    int      `json:"timeout"`    // ms; 0 = default (60000)
	Lang       string   `json:"lang"`       // BCP 47 locale e.g. "da-DK" (mode=local)
	ExtraFlags []string `json:"extraFlags"` // additional Chrome CLI flags (mode=local)
}

// RemoteBrowser is a saved remote browser script with its connection config.
type RemoteBrowser struct {
	ID          nullable.Nullable[uuid.UUID]             `json:"id"`
	CreatedAt   *time.Time                               `json:"createdAt"`
	UpdatedAt   *time.Time                               `json:"updatedAt"`
	CompanyID   nullable.Nullable[uuid.UUID]             `json:"companyID"`
	Name        nullable.Nullable[vo.String64]           `json:"name"`
	Description nullable.Nullable[vo.OptionalString1024] `json:"description"`
	Script      nullable.Nullable[vo.String1MB]          `json:"script"`
	Config      nullable.Nullable[RemoteBrowserConfig]   `json:"config"`

	Company *Company `json:"-"`
}

// Validate checks required fields and config values.
func (m *RemoteBrowser) Validate() error {
	if err := validate.NullableFieldRequired("name", m.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("script", m.Script); err != nil {
		return err
	}
	if m.Config.IsSpecified() {
		if cfg, err := m.Config.Get(); err == nil {
			if cfg.Mode != "" && cfg.Mode != "local" && cfg.Mode != "remote" {
				return fmt.Errorf("config.mode must be 'local' or 'remote'")
			}
			if cfg.Mode == "remote" && cfg.Remote == "" {
				return fmt.Errorf("config.remote is required when mode is 'remote'")
			}
		}
	}
	return nil
}

// ToDBMap returns the fields that should be persisted.
func (m *RemoteBrowser) ToDBMap() map[string]any {
	dbMap := map[string]any{}
	if m.Name.IsSpecified() {
		dbMap["name"] = nil
		if name, err := m.Name.Get(); err == nil {
			dbMap["name"] = name.String()
		}
	}
	if m.Description.IsSpecified() {
		dbMap["description"] = nil
		if description, err := m.Description.Get(); err == nil {
			dbMap["description"] = description.String()
		}
	}
	if m.Script.IsSpecified() {
		dbMap["script"] = nil
		if script, err := m.Script.Get(); err == nil {
			dbMap["script"] = script.String()
		}
	}
	if m.Config.IsSpecified() {
		dbMap["config"] = ""
		if cfg, err := m.Config.Get(); err == nil {
			if b, err := json.Marshal(cfg); err == nil {
				dbMap["config"] = string(b)
			}
		}
	}
	if m.CompanyID.IsSpecified() {
		if m.CompanyID.IsNull() {
			dbMap["company_id"] = nil
		} else {
			dbMap["company_id"] = m.CompanyID.MustGet()
		}
	}
	return dbMap
}

// RemoteBrowserOverview is a lightweight listing model.
type RemoteBrowserOverview struct {
	ID          uuid.UUID  `json:"id,omitempty"`
	CreatedAt   *time.Time `json:"createdAt"`
	UpdatedAt   *time.Time `json:"updatedAt"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	CompanyID   *uuid.UUID `json:"companyID"`
}
