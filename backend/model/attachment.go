package model

import (
	"mime/multipart"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

type Attachment struct {
	ID              nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt       *time.Time                              `json:"createdAt"`
	UpdatedAt       *time.Time                              `json:"updatedAt"`
	CompanyID       nullable.Nullable[uuid.UUID]            `json:"companyID"`
	Name            nullable.Nullable[vo.OptionalString127] `json:"name"`
	Description     nullable.Nullable[vo.OptionalString255] `json:"description"`
	FileName        nullable.Nullable[vo.FileName]          `json:"fileName"`
	EmbeddedContent nullable.Nullable[bool]                 `json:"embeddedContent"`
	// path is the calculated path to the file
	Path nullable.Nullable[vo.RelativeFilePath] `json:"path"`
	// used in the API to upload files
	File *multipart.FileHeader `json:"-"`
}

func (a *Attachment) Validate() error {
	if err := validate.NullableFieldRequired("name", a.Name); err != nil {
		return err
	}
	return nil
}

func (a *Attachment) ToDBMap() map[string]any {
	m := map[string]any{}
	if a.CompanyID.IsSpecified() {
		if a.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = a.CompanyID.MustGet()
		}
	}
	if a.Name.IsSpecified() {
		m["name"] = nil
		if name, err := a.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if a.Description.IsSpecified() {
		m["description"] = nil
		if description, err := a.Description.Get(); err == nil {
			m["description"] = description.String()
		}
	}
	if a.FileName.IsSpecified() {
		m["filename"] = nil
		if fileName, err := a.FileName.Get(); err == nil {
			m["filename"] = fileName.String()
		}
		// if name is not set, use file name
		if m["name"] == nil {
			m["name"] = m["filename"]
		}
	}
	if a.Path.IsSpecified() {
		m["path"] = nil
		if path, err := a.Path.Get(); err == nil {
			m["path"] = path.String()
		}
	}
	if v, err := a.EmbeddedContent.Get(); err == nil {
		m["embedded_content"] = v
	}
	return m
}
