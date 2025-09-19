package app

import (
	"github.com/phishingclub/phishingclub/controller"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Controllers is a collection of controllers
type Controllers struct {
	Asset             *controller.Asset
	Attachment        *controller.Attachment
	Company           *controller.Company
	Health            *controller.Health
	Installer         *controller.Install
	InitialSetup      *controller.InitialSetup
	Page              *controller.Page
	Log               *controller.Log
	Option            *controller.Option
	User              *controller.User
	Domain            *controller.Domain
	Recipient         *controller.Recipient
	RecipientGroup    *controller.RecipientGroup
	SMTPConfiguration *controller.SMTPConfiguration
	Email             *controller.Email
	CampaignTemplate  *controller.CampaignTemplate
	Campaign          *controller.Campaign
	QR                *controller.QRGenerator
	APISender         *controller.APISender
	AllowDeny         *controller.AllowDeny
	Webhook           *controller.Webhook
	Identifier        *controller.Identifier
	Version           *controller.Version
	SSO               *controller.SSO
	Update            *controller.Update
	Import            *controller.Import
	Backup            *controller.Backup
}

// NewControllers creates a collection of controllers
func NewControllers(
	staticAssetPath string,
	attachmentsPath string,
	repositories *Repositories,
	services *Services,
	logger *zap.SugaredLogger,
	atomLogger *zap.AtomicLevel,
	utillities *Utilities,
	db *gorm.DB,
) *Controllers {
	common := controller.Common{
		SessionService: services.Session,
		Logger:         logger,
		Response:       utillities.JSONResponseHandler,
	}
	asset := &controller.Asset{
		Common:          common,
		StaticAssetPath: staticAssetPath,
		AssetService:    services.Asset,
		OptionService:   services.Option,
		DomainService:   services.Domain,
	}
	attachment := &controller.Attachment{
		Common:               common,
		StaticAttachmentPath: attachmentsPath,
		AttachmentService:    services.Attachment,
		OptionService:        services.Option,
		TemplateService:      services.Template,
		CompanyService:       services.Company,
	}
	company := &controller.Company{
		Common:           common,
		CampaignService:  services.Campaign,
		CompanyService:   services.Company,
		RecipientService: services.Recipient,
	}
	initialSetup := &controller.InitialSetup{
		Common:           common,
		CLIOutputter:     utillities.CLIOutputter,
		OptionRepository: repositories.Option,
		InstallService:   services.InstallSetup,
		OptionService:    services.Option,
	}
	installer := &controller.Install{
		Common:            common,
		UserRepository:    repositories.User,
		CompanyRepository: repositories.Company,
		OptionRepository:  repositories.Option,
		PasswordHasher:    *utillities.PasswordHasher,
		DB:                db,
	}
	health := &controller.Health{}
	log := &controller.Log{
		Common:        common,
		OptionService: services.Option,
		Database:      db,
		LoggerAtom:    atomLogger,
	}
	page := &controller.Page{
		Common:          common,
		PageService:     services.Page,
		TemplateService: services.Template,
	}
	option := &controller.Option{
		Common:        common,
		OptionService: services.Option,
	}
	user := &controller.User{
		Common:      common,
		UserService: services.User,
	}
	domain := &controller.Domain{
		Common:        common,
		DomainService: services.Domain,
	}
	recipient := &controller.Recipient{
		Common:           common,
		RecipientService: services.Recipient,
	}
	recipientGroup := &controller.RecipientGroup{
		Common:                common,
		RecipientGroupService: services.RecipientGroup,
	}
	smtpConfiguration := &controller.SMTPConfiguration{
		Common:                   common,
		SMTPConfigurationService: services.SMTPConfiguration,
	}
	email := &controller.Email{
		Common:          common,
		EmailService:    services.Email,
		TemplateService: services.Template,
		EmailRepository: repositories.Email,
	}
	campaignTemplate := &controller.CampaignTemplate{
		Common:                  common,
		CampaignTemplateService: services.CampaignTemplate,
	}
	campaign := &controller.Campaign{
		Common:          common,
		CampaignService: services.Campaign,
	}
	qr := &controller.QRGenerator{
		Common: common,
	}
	apiSender := &controller.APISender{
		Common:           common,
		APISenderService: services.APISender,
	}
	allowDeny := &controller.AllowDeny{
		Common:           common,
		AllowDenyService: services.AllowDeny,
	}
	webhook := &controller.Webhook{
		Common:         common,
		WebhookService: services.Webhook,
	}
	identifier := &controller.Identifier{
		Common:            common,
		IdentifierService: services.Identifier,
	}
	version := &controller.Version{Common: common}
	sso := &controller.SSO{Common: common, SSO: services.SSO}
	update := &controller.Update{
		Common:        common,
		UpdateService: services.Update,
		OptionService: services.Option,
	}
	importController := &controller.Import{
		Common:        common,
		ImportService: services.Import,
	}
	backup := &controller.Backup{
		Common:        common,
		BackupService: services.Backup,
	}

	return &Controllers{
		Asset:             asset,
		Attachment:        attachment,
		Company:           company,
		Installer:         installer,
		InitialSetup:      initialSetup,
		Health:            health,
		Page:              page,
		Log:               log,
		Option:            option,
		User:              user,
		Domain:            domain,
		Recipient:         recipient,
		RecipientGroup:    recipientGroup,
		SMTPConfiguration: smtpConfiguration,
		Email:             email,
		CampaignTemplate:  campaignTemplate,
		Campaign:          campaign,
		QR:                qr,
		APISender:         apiSender,
		AllowDeny:         allowDeny,
		Webhook:           webhook,
		Identifier:        identifier,
		Version:           version,
		SSO:               sso,
		Update:            update,
		Import:            importController,
		Backup:            backup,
	}
}
