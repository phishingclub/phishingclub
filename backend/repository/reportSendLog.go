package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

var reportSendLogAllowedColumns = assignTableToColumns(database.REPORT_SEND_LOG_TABLE, []string{
	"created_at",
	"status",
	"trigger",
	"campaign_name",
	"group_name",
	"recipient_count",
})

// ReportSendLogOption is for query options
type ReportSendLogOption struct {
	*vo.QueryArgs
}

// ReportSendLog is the repository for report delivery logs
type ReportSendLog struct {
	DB *gorm.DB
}

// Insert inserts a report send log row
func (r *ReportSendLog) Insert(
	ctx context.Context,
	log *model.ReportSendLog,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := log.ToDBMap()
	row["id"] = id
	// the log is append only and has no updated_at column
	row["created_at"] = utils.NowRFC3339UTC()

	res := r.DB.
		Model(&database.ReportSendLog{}).
		Create(row)

	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return &id, nil
}

// GetAllByCompanyID returns the report send logs for a company using pagination
func (r *ReportSendLog) GetAllByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ReportSendLogOption,
) (*model.Result[model.ReportSendLog], error) {
	result := model.NewEmptyResult[model.ReportSendLog]()
	var rows []database.ReportSendLog
	db := whereCompany(r.DB, database.REPORT_SEND_LOG_TABLE, companyID)
	db, err := useQuery(db, database.REPORT_SEND_LOG_TABLE, options.QueryArgs, reportSendLogAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if res := db.Find(&rows); res.Error != nil {
		return result, errs.Wrap(res.Error)
	}
	hasNextPage, err := useHasNextPage(db, database.REPORT_SEND_LOG_TABLE, options.QueryArgs, reportSendLogAllowedColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	result.HasNextPage = hasNextPage
	for _, row := range rows {
		result.Rows = append(result.Rows, ToReportSendLog(&row))
	}
	return result, nil
}

// ToReportSendLog maps a database row to the business model
func ToReportSendLog(row *database.ReportSendLog) *model.ReportSendLog {
	id := nullable.NewNullableWithValue(*row.ID)

	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	}
	campaignID := nullable.NewNullNullable[uuid.UUID]()
	if row.CampaignID != nil {
		campaignID.Set(*row.CampaignID)
	}

	return &model.ReportSendLog{
		ID:             id,
		CreatedAt:      row.CreatedAt,
		CompanyID:      companyID,
		CampaignID:     campaignID,
		CampaignName:   row.CampaignName,
		GroupName:      row.GroupName,
		Trigger:        row.Trigger,
		Status:         row.Status,
		RecipientCount: row.RecipientCount,
		Recipients:     row.Recipients,
		SenderEmail:    row.SenderEmail,
		ErrorMessage:   row.ErrorMessage,
	}
}
