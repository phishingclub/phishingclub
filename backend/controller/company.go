package controller

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// DomainColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var CompanyColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.COMPANY_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.COMPANY_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.COMPANY_TABLE, "name"),
}

// Company is a Company controller
type Company struct {
	Common
	CompanyService   *service.Company
	CampaignService  *service.Campaign
	RecipientService *service.Recipient
}

// GetByID gets a company by id
func (c *Company) GetByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	companyID, err := uuid.Parse(g.Param("id"))
	if err != nil {
		// ignore err as caused by bad user input
		_ = err
		c.Response.BadRequestMessage(g, api.InvalidCompanyID)
		return
	}
	// get company
	ctx := g.Request.Context()
	company, err := c.CompanyService.GetByID(
		ctx,
		session,
		&companyID,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, company)
}

// ExportByCompanyID outputs a CSV with all events related to the recipient
func (c *Company) ExportByCompanyID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get the company exported
	company, err := c.CompanyService.GetByID(
		g,
		session,
		companyID,
	)
	// create ZIP file in memory
	zipBuffer := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuffer)
	zipFileName := fmt.Sprintf("company_export_%s.zip", company.Name.MustGet().String())

	// add company data to zip
	{
		buffer := &bytes.Buffer{}
		writer := csv.NewWriter(buffer)
		headers := []string{
			"Created at",
			"Updated at",
			"Name",
		}
		err = writer.Write(headers)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		row := []string{
			utils.CSVFromDate(company.CreatedAt),
			utils.CSVFromDate(company.UpdatedAt),
			utils.CSVRemoveFormulaStart(utils.NullableToString(company.Name)),
		}
		err = writer.Write(row)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		writer.Flush()
		// add to zip
		f, err := zipWriter.Create("company.csv")
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		_, err = f.Write(buffer.Bytes())
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}

	// add recipients to zip
	{
		// get the recipients
		recipients, err := c.RecipientService.GetByCompanyID(
			g,
			session,
			companyID,
			&repository.RecipientOption{
				WithCompany: true,
				WithGroups:  true,
			},
		)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		// write a csv buffer with all recipient and their groups
		buffer := &bytes.Buffer{}
		writer := csv.NewWriter(buffer)
		headers := []string{
			"Created at",
			"Updated at",
			"Email",
			"Phone",
			"Extra Identifier",
			"Name",
			"Position",
			"Department",
			"City",
			"Country",
			"Misc",
		}
		// find the recipient with the most groups and add that number of
		// extra headers for groups
		maxGroups := 0
		for _, recipient := range recipients.Rows {
			groups, _ := recipient.Groups.Get()
			if groupLen := len(groups); groupLen > maxGroups {
				maxGroups = groupLen
			}
		}
		for i := 1; i <= maxGroups; i++ {
			headers = append(headers, fmt.Sprintf("Group %d", i))
		}
		err = writer.Write(headers)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		for _, recipient := range recipients.Rows {
			groups, _ := recipient.Groups.Get()
			row := []string{
				utils.CSVFromDate(recipient.CreatedAt),
				utils.CSVFromDate(recipient.UpdatedAt),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Email)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Phone)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.ExtraIdentifier)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.FirstName)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.LastName)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Position)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Department)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.City)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Country)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Misc)),
			}
			for _, group := range groups {
				row = append(row, group.Name.MustGet().String())
			}
			err = writer.Write(row)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			writer.Flush()
		}
		// add to zip
		f, err := zipWriter.Create("recipients.csv")
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		_, err = f.Write(buffer.Bytes())
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	// get all campaigns all recipient events
	{
		campaigns, err := c.CampaignService.GetByCompanyID(
			g,
			session,
			companyID,
			&repository.CampaignOption{},
		)
		for _, campaign := range campaigns.Rows {
			headers := []string{
				"Campaign",
				"Created at",
				"Recipient name",
				"Recipient email",
				"Event name",
				"Event Details",
				"User-Agent",
				"IP",
			}
			buffer := &bytes.Buffer{}
			writer := csv.NewWriter(buffer)
			err = writer.Write(headers)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			queryArgs := vo.QueryArgs{}
			queryArgs.OrderBy = repository.TableColumn(
				database.CAMPAIGN_EVENT_TABLE,
				"created_at",
			)
			sortOrder := g.DefaultQuery("sortOrder", "desc")
			if sortOrder == "desc" {
				queryArgs.Desc = true
			}
			// get all rows
			queryArgs.Limit = 0
			queryArgs.Offset = 0
			// get events by campaign id
			cid := campaign.ID.MustGet()
			events, err := c.CampaignService.GetEventsByCampaignID(
				g.Request.Context(),
				session,
				&cid,
				&queryArgs,
				nil,
				nil,
			)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			for _, event := range events.Rows {
				firstName := "anonymized"
				lastName := "anonymized"
				recpEmail := "anonymized"
				if event.Recipient != nil {
					firstName = event.Recipient.FirstName.MustGet().String()
					lastName = event.Recipient.LastName.MustGet().String()
					recpEmail = event.Recipient.Email.MustGet().String()
				}
				row := []string{
					utils.CSVRemoveFormulaStart(campaign.Name.MustGet().String()),
					utils.CSVFromDate(event.CreatedAt),
					utils.CSVRemoveFormulaStart(firstName),
					utils.CSVRemoveFormulaStart(lastName),
					utils.CSVRemoveFormulaStart(recpEmail),
					utils.CSVRemoveFormulaStart(cache.EventNameByID[event.EventID.String()]),
					utils.CSVRemoveFormulaStart(event.Data.String()),
					utils.CSVRemoveFormulaStart(event.UserAgent.String()),
					utils.CSVRemoveFormulaStart(event.IP.String()),
				}
				err = writer.Write(row)
				if ok := c.handleErrors(g, err); !ok {
					return
				}
			}
			// add a new subdirectory wit the event file in the zip
			writer.Flush()
			// add to zip
			filename := fmt.Sprintf("campaign_events/%s.csv", campaign.Name.MustGet().String())
			f, err := zipWriter.Create(filename)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			_, err = f.Write(buffer.Bytes())
			if ok := c.handleErrors(g, err); !ok {
				return
			}
		}
	}
	// close zip
	err = zipWriter.Close()
	if ok := c.handleErrors(g, err); !ok {
		return
	}

	c.responseWithZIP(g, zipBuffer, zipFileName)
}

// ExportShared outputs a CSV with all shared recipients and events
func (c *Company) ExportShared(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// create ZIP file in memory
	zipBuffer := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuffer)
	zipFileName := "shared_export_%s.zip"
	// add recipients to zip
	{
		// get the recipients
		recipients, err := c.RecipientService.GetByCompanyID(
			g,
			session,
			nil,
			&repository.RecipientOption{
				WithCompany: true,
				WithGroups:  true,
			},
		)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		// write a csv buffer with all recipient and their groups
		buffer := &bytes.Buffer{}
		writer := csv.NewWriter(buffer)
		headers := []string{
			"Created at",
			"Updated at",
			"Email",
			"Phone",
			"Extra Identifier",
			"Name",
			"Position",
			"Department",
			"City",
			"Country",
			"Misc",
		}
		// find the recipient with the most groups and add that number of
		// extra headers for groups
		maxGroups := 0
		for _, recipient := range recipients.Rows {
			groups, _ := recipient.Groups.Get()
			if groupLen := len(groups); groupLen > maxGroups {
				maxGroups = groupLen
			}
		}
		for i := 1; i <= maxGroups; i++ {
			headers = append(headers, fmt.Sprintf("Group %d", i))
		}
		err = writer.Write(headers)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		for _, recipient := range recipients.Rows {
			groups, _ := recipient.Groups.Get()
			row := []string{
				utils.CSVFromDate(recipient.CreatedAt),
				utils.CSVFromDate(recipient.UpdatedAt),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Email)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Phone)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.ExtraIdentifier)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.FirstName)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.LastName)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Position)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Department)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.City)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Country)),
				utils.CSVRemoveFormulaStart(utils.NullableToString(recipient.Misc)),
			}
			for _, group := range groups {
				row = append(row, group.Name.MustGet().String())
			}
			err = writer.Write(row)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			writer.Flush()
		}
		// add to zip
		f, err := zipWriter.Create("recipients.csv")
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		_, err = f.Write(buffer.Bytes())
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	// get all campaigns all recipient events
	{
		campaigns, err := c.CampaignService.GetByCompanyID(
			g,
			session,
			nil,
			&repository.CampaignOption{},
		)
		for _, campaign := range campaigns.Rows {
			headers := []string{
				"Campaign",
				"Created at",
				"Recipient name",
				"Recipient email",
				"Event name",
				"Event Details",
				"User-Agent",
				"IP",
			}
			buffer := &bytes.Buffer{}
			writer := csv.NewWriter(buffer)
			err = writer.Write(headers)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			queryArgs := vo.QueryArgs{}
			queryArgs.OrderBy = repository.TableColumn(
				database.CAMPAIGN_EVENT_TABLE,
				"created_at",
			)
			sortOrder := g.DefaultQuery("sortOrder", "desc")
			if sortOrder == "desc" {
				queryArgs.Desc = true
			}
			// get all rows
			queryArgs.Limit = 0
			queryArgs.Offset = 0
			// get events by campaign id
			cid := campaign.ID.MustGet()
			events, err := c.CampaignService.GetEventsByCampaignID(
				g.Request.Context(),
				session,
				&cid,
				&queryArgs,
				nil,
				nil,
			)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			for _, event := range events.Rows {
				firstName := "anonymized"
				lastName := "anonymized"
				recpEmail := "anonymized"
				if event.Recipient != nil {
					firstName = event.Recipient.FirstName.MustGet().String()
					lastName = event.Recipient.LastName.MustGet().String()
					recpEmail = event.Recipient.Email.MustGet().String()
				}
				row := []string{
					utils.CSVRemoveFormulaStart(campaign.Name.MustGet().String()),
					utils.CSVFromDate(event.CreatedAt),
					utils.CSVRemoveFormulaStart(firstName),
					utils.CSVRemoveFormulaStart(lastName),
					utils.CSVRemoveFormulaStart(recpEmail),
					utils.CSVRemoveFormulaStart(cache.EventNameByID[event.EventID.String()]),
					utils.CSVRemoveFormulaStart(event.Data.String()),
					utils.CSVRemoveFormulaStart(event.UserAgent.String()),
					utils.CSVRemoveFormulaStart(event.IP.String()),
				}
				err = writer.Write(row)
				if ok := c.handleErrors(g, err); !ok {
					return
				}
			}
			// add a new subdirectory wit the event file in the zip
			writer.Flush()
			// add to zip
			filename := fmt.Sprintf("campaign_events/%s.csv", campaign.Name.MustGet().String())
			f, err := zipWriter.Create(filename)
			if ok := c.handleErrors(g, err); !ok {
				return
			}
			_, err = f.Write(buffer.Bytes())
			if ok := c.handleErrors(g, err); !ok {
				return
			}
		}
	}
	// close zip
	err := zipWriter.Close()
	if ok := c.handleErrors(g, err); !ok {
		return
	}

	c.responseWithZIP(g, zipBuffer, zipFileName)
}

// ChangeName changes a company name
func (c *Company) ChangeName(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.Company
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// change company name
	err := c.CompanyService.UpdateByID(
		g,
		session,
		id,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, nil)
}

// SoftDelete soft deletes a company
func (c *Company) DeleteByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// TODO company delete should FAIL if it has any relations to anything
	// delete company
	_, err := c.CompanyService.DeleteByID(g, session, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Create creates a company
func (c *Company) Create(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.Company
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// save company
	ctx := g.Request.Context()
	company, err := c.CompanyService.Create(
		ctx,
		session,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{
		"id": company.ID,
	})
}

// GetAll gets all companies with pagination
func (c *Company) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(CompanyColumnsMap)
	// get companies
	ctx := g.Request.Context()
	companies, err := c.CompanyService.GetAll(
		ctx,
		session,
		queryArgs,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, companies)
}
