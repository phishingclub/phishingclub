package main

import (
	"context"
	"flag"
	"fmt"
	golog "log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "embed"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/go-errors/errors"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/acme"
	"github.com/phishingclub/phishingclub/app"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/cli"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/install"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/seed"
	"github.com/phishingclub/phishingclub/sso"
	"github.com/phishingclub/phishingclub/task"
	"github.com/phishingclub/phishingclub/version"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

const (
	APP_NAME = "Phishing Club"
)

var (
	flagInstall                 = flag.Bool("install", false, "Install as a systemd service (interactive)")
	flagInstallNonInteractive   = flag.Bool("install-non-interactive", false, "Install as a systemd service without interactive prompts")
	flagUpdate                  = flag.Bool("update", false, "Update the application binary and restart the service")
	flagUninstall               = flag.Bool("uninstall", false, "Uninstall the application with confirmation prompt")
	flagUninstallNonInteractive = flag.Bool("uninstall-non-interactive", false, "Uninstall the application without confirmation prompt")
	flagSystemd                 = flag.Bool("systemd", false, "Indicates the application is running as a systemd service, this flag is only functional on the initial boot when seeding the database.")
	flagVersion                 = flag.Bool("version", false, "Show version")
	flagConfigPath              = flag.String("config", "./config.json", "Path to config file")
	flagFilePath                = flag.String("files", "./data", "Path to save application data")
	env                         = flag.Bool("env", false, "Outputs the available environment variables")
	flagRecovery                = flag.Bool("recover", false, "Used for interactive recovery of an account")
	flagConfigOnly              = flag.Bool("config-only", false, "Run interactive installer and save config without installing")
	flagDebug                   = flag.Bool("debug", false, "Force debug logging on db and app logger, ignores db settings on startup")
)

func main() {
	flag.Parse()

	if *env {
		cli.OutputEnv()
		return
	}

	if *flagVersion {
		cli.PrintVersion(APP_NAME, version.Get())
		return
	}

	if *flagConfigOnly {
		if err := install.RunInteractiveConfigOnly(*flagConfigPath); err != nil {
			golog.Fatalf("Config generation failed: %s", err)
		}
		return
	}

	if *flagInstall {
		if err := install.Install(); err != nil {
			golog.Fatalf("Installation failed: %s", err)
		}
		return
	}

	if *flagInstallNonInteractive {
		if err := install.InstallNonInteractive(); err != nil {
			golog.Fatalf("Installation failed: %s", err)
		}
		return
	}

	if *flagUninstall {
		if err := install.Uninstall(); err != nil {
			golog.Fatalf("Uninstallation failed: %s", err)
		}
		return
	}

	if *flagUninstallNonInteractive {
		if err := install.UninstallNonInteractive(); err != nil {
			golog.Fatalf("Uninstallation failed: %s", err)
		}
		return
	}

	if *flagUpdate {
		if err := install.Update(); err != nil {
			golog.Fatalf("Update failed: %s", err)
		}
		return
	}

	buildMode := app.MODE_DEVELOPMENT
	if build.Flags.Production {
		buildMode = app.MODE_PRODUCTION
	}

	// check if the files path ends with / else add it
	if (*flagFilePath)[len(*flagFilePath)-1:] != "/" {
		*flagFilePath = *flagFilePath + "/"
	}
	acmeCertPath := fmt.Sprintf("%scerts", *flagFilePath)
	ownManagedTLSPath := fmt.Sprintf("%scerts/own-managed", *flagFilePath)
	assetPath := fmt.Sprintf("%sassets", *flagFilePath)
	attachmentsPath := fmt.Sprintf("%sattachments", *flagFilePath)

	// print banner and version
	cli.PrintBanner()
	cli.PrintVersion(APP_NAME, version.Get())
	// get conf
	conf, err := app.SetupConfig(
		buildMode,
		*flagConfigPath,
	)
	if err != nil {
		golog.Fatalf("failed to setup config: %s", err)
	}
	// setup database connection
	db, err := app.SetupDatabase(conf)
	if err != nil {
		golog.Fatalf("Failed to connect to database: %s\nDSN: %s", err, conf.Database().DSN)
	}
	logger, atomicLogger, err := app.SetupLogger(buildMode, conf)
	if err != nil {
		golog.Fatalf("failed to setup logger: %s", err)
	}
	defer func() {
		_ = logger.Sync()
	}()
	// set log levels
	err = setLogLevels(db, atomicLogger, *flagDebug)
	if err != nil {
		// this could fail due to db not being seeded
		db.Logger = db.Logger.LogMode(gormLogger.Silent)
		atomicLogger.SetLevel(zap.InfoLevel)
	}
	// set license server url
	licenseServer := data.GetCrmURL()
	// output debug information
	/*
		wd, err := os.Getwd()
		if err != nil {
			logger.Fatalw("Failed to get working directory", "error", err)
		}
		usr, err := user.Current()
		if err != nil {
			logger.Fatalw("Failed to get current user", "error", err)
		}
		// setup configuration
		logger.Debugw("debug",
			"applicationMode", buildMode,
			"working directory", wd,
			"OS user", usr.Username,
			"pathConfig", *flagConfigPath,
		)
	*/
	if p := conf.LogPath; len(p) > 0 {
		logger.Debugw("using log file", "path", p)
	}
	if p := conf.ErrLogPath; len(p) > 0 {
		logger.Debugw("using error log file", "path", p)
	}
	// gin is always set to production mode
	if buildMode == app.MODE_DEVELOPMENT {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// setup utils and repos
	utils := app.NewUtils()
	repositories := app.NewRepositories(db)
	// run migrations and seeding, including development seeding
	// Use systemd flag to indicate this was installed via systemd
	usingSystemd := *flagInstall || *flagSystemd
	err = seed.InitialInstallAndSeed(db, repositories, logger, usingSystemd)
	if err != nil {
		logger.Fatalw("Failed to run migrations and seeding", "error", err)
	}
	// setup logging again so it is according to the database
	err = setLogLevels(db, atomicLogger, *flagDebug)
	if err != nil {
		// this could fail due to db not being seeded
		db.Logger = db.Logger.LogMode(gormLogger.Silent)
		atomicLogger.SetLevel(zap.InfoLevel)
	}
	// setup cert magic for TLS cert handling
	certMagicConfig, certMagicCache, err := acme.SetupCertMagic(
		acmeCertPath+"/acme",
		conf,
		db,
		logger,
	)
	if err != nil {
		logger.Errorw("failed to setup certmagic", "error", err)
		return
	}
	// setup services, middleware and controllers
	services := app.NewServices(
		db,
		repositories,
		logger,
		utils,
		assetPath,
		attachmentsPath,
		ownManagedTLSPath,
		buildMode,
		certMagicConfig,
		certMagicCache,
		licenseServer,
		*flagFilePath,
	)
	// get entra-id options and setup msal client
	ssoOpt, err := services.SSO.GetSSOOptionWithoutAuth(context.Background())
	if err != nil {
		logger.Errorw("failed to setup sso", "error", err)
		return
	}
	if ssoOpt.Enabled {
		services.SSO.MSALClient, err = sso.NewEntreIDClient(ssoOpt)
		if err != nil && !errors.Is(err, errs.ErrSSODisabled) {
			logger.Errorw("failed to setup msal client", "error", err)
			return
		}

	}
	middlewares := app.NewMiddlewares(
		1,
		1,
		conf,
		services,
		utils,
		logger,
	)
	controllers := app.NewControllers(
		assetPath,
		attachmentsPath,
		repositories,
		services,
		logger,
		atomicLogger,
		utils,
		db,
		conf,
	)
	// setup admin account
	isInstalled, err := controllers.InitialSetup.IsInstalled(context.Background())
	if err != nil {
		logger.Fatalw("failed to check if app is installed", "error", err)
	}
	if !isInstalled {
		err := controllers.InitialSetup.HandleInitialSetup(context.Background())
		if err != nil {
			logger.Fatalw("failed to handle the installers initial setup", "error", err)
		}
	}
	// TODO run migrations for existing databases

	// interactive account recovery
	if *flagRecovery {
		interactiveAccountRecovery(repositories, utils)
		return
	}
	// setup administration server
	var adminRouter *gin.Engine
	if !build.Flags.Production {
		adminRouter = gin.Default()
	} else {
		adminRouter = gin.New()
		adminRouter.Use(ginzap.GinzapWithConfig(logger.Desugar(), &ginzap.Config{
			TimeFormat: time.RFC3339,
			UTC:        true,
			Context: ginzap.Fn(func(c *gin.Context) []zapcore.Field {
				fields := []zapcore.Field{}
				fields = append(fields, zap.String("host", c.Request.Host))
				fields = append(fields, zap.String("server", "admin"))

				return fields
			}),
		}))
		adminRouter.Use(ginzap.RecoveryWithZap(logger.Desugar(), true))
		// dont trust x-forwarded-by by default
		if len(conf.IPSecurity.TrustedProxies) > 0 {
			err := adminRouter.SetTrustedProxies(conf.IPSecurity.TrustedProxies)
			if err != nil {
				logger.Fatalw("failed to set trusted proxies", "error", err)
			}
		} else {
			err := adminRouter.SetTrustedProxies(nil)
			if err != nil {
				logger.Fatalw("failed to set trusted proxies", "error", err)
			}
		}
		// trust specific headers
		adminRouter.TrustedPlatform = conf.IPSecurity.TrustedIPHeader
		logger.Debugw("admin IP security",
			"admin_allowed", strings.Join(conf.IPSecurity.AdminAllowed, ","),
			"trusted_proxies", strings.Join(conf.IPSecurity.TrustedProxies, ","),
			"trusted_ip_header", conf.IPSecurity.TrustedIPHeader,
		)
	}
	adminRouter.Use(middlewares.IPLimiter)
	adminServer := app.NewAdministrationServer(
		adminRouter,
		controllers,
		middlewares,
		logger,
		certMagicConfig,
		build.Flags.Production,
	)
	adminStartupChannel, adminListener, err := adminServer.StartServer(conf)
	if err != nil {
		logger.Fatalw("Failed to start admin server", "error", err)
	}

	adminStartupResult := <-adminStartupChannel
	if !adminStartupResult.Success && adminStartupResult.Error != nil {
		logger.Fatalw("Failed to start admin server", "error", adminStartupResult.Error)
	}

	// update the config with the actual port if the port was 0
	if conf.AdminNetAddressPort() == 0 {
		err := conf.SetAdminNetAddress(adminListener.Addr().String())
		if err != nil {
			logger.Fatalw("failed to set admin net address", "error", err)
		}
		err = conf.WriteToFile(*flagConfigPath)
		if err != nil {
			logger.Fatalw("failed to write config", "error", err)
		}
	}
	// startup message
	cli.PrintServerStarted("Admin server", adminListener.Addr().String())
	// start the phishing servers (HTTP and HTTPS)
	phishingServer := app.NewServer(
		assetPath,
		ownManagedTLSPath,
		db,
		controllers,
		services,
		repositories,
		logger,
		certMagicConfig,
	)

	var r *gin.Engine
	if !build.Flags.Production {
		r = gin.Default()
	} else {
		r = gin.New()
		r.Use(ginzap.GinzapWithConfig(logger.Desugar(), &ginzap.Config{
			TimeFormat: time.RFC3339,
			UTC:        true,
			Context: ginzap.Fn(func(c *gin.Context) []zapcore.Field {
				fields := []zapcore.Field{}
				fields = append(fields, zap.String("host", c.Request.Host))
				fields = append(fields, zap.String("server", "admin"))

				return fields
			}),
		}))
		r.Use(ginzap.RecoveryWithZap(logger.Desugar(), true))
	}

	r.Use(ginzap.GinzapWithConfig(logger.Desugar(), &ginzap.Config{
		TimeFormat: time.RFC3339,
		UTC:        true,
		Context: ginzap.Fn(func(c *gin.Context) []zapcore.Field {
			fields := []zapcore.Field{}
			fields = append(fields, zap.String("host", c.Request.Host))
			fields = append(fields, zap.String("server", "phishing"))

			return fields
		}),
	}))
	r.Use(ginzap.RecoveryWithZap(logger.Desugar(), true))
	phishingServer.AssignRoutes(r)
	// start the HTTP server
	httpTestChan, httpListener, err := phishingServer.StartHTTP(r, conf)
	if err != nil {
		logger.Fatalw("failed to start phishing HTTP server", "error", err)
	}
	httpTestResult := <-httpTestChan
	if !httpTestResult.Success && httpTestResult.Error != nil {
		logger.Fatalw("failed to start phishing HTTP server", "error", httpTestResult.Error)
	}
	cli.PrintServerStarted("Phishing HTTP server", httpListener.Addr().String())
	// start the HTTPSserver
	httpsTestChan, httpsListener, err := phishingServer.StartHTTPS(r, conf)
	if err != nil {
		logger.Fatalw("failed to start HTTPS phishing server", "error", err)
	}
	httpsTestResult := <-httpsTestChan
	if !httpsTestResult.Success && httpsTestResult.Error != nil {
		logger.Fatalw("failed to start HTTPS phishing server", "error", httpsTestResult.Error)
	}
	cli.PrintServerStarted("Phishing HTTPS server", httpsListener.Addr().String())

	// start the task handler
	systemSession, err := model.NewSystemSession()
	if err != nil {
		logger.Fatalw("Failed to load system user", "error", err)
	}
	daemon := task.Runner{
		CampaignService: services.Campaign,
		UpdateService:   services.Update,
		Logger:          logger,
	}

	// start tasks runner
	// let the system tasks run once before starting the normal work tasks
	// this ensure that a license check is completed before attempting to send out
	// e-mails as that would cancel the e-mail delivery.
	daemonCtx, cancelDaemons := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go daemon.RunSystemTasks(
		daemonCtx,
		systemSession,
		&wg,
	)
	wg.Wait()
	go daemon.Run(
		daemonCtx,
		systemSession,
	)

	// handle aborts and abort signals

	abort := make(chan struct{})
	abortSignalCh := make(chan os.Signal, 1)
	signal.Notify(abortSignalCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGABRT)

	// listen for abort signals
	go func() {
		sig := <-abortSignalCh
		logger.Warnw("Received abort signal - initiating graceful shutdown",
			"signal", sig,
		)

		// Create context with timeout for shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Graceful shutdown for daemons
		logger.Debugf("Stopping daemons")
		cancelDaemons()

		// Graceful shutdown for admin server
		logger.Debugf("Stopping administration server")
		if err := adminServer.Server.Shutdown(ctx); err != nil {
			logger.Errorw("Admin server shutdown error", "error", err)
		}

		// Graceful shutdown for HTTP server
		logger.Debugf("Stopping HTTP Phishing server")
		if err := phishingServer.HTTPServer.Shutdown(ctx); err != nil {
			logger.Errorw("HTTP server shutdown error", "error", err)
		}

		// Graceful shutdown for HTTPS server
		logger.Debugf("Stopping HTTPS Phishing server")
		if err := phishingServer.HTTPSServer.Shutdown(ctx); err != nil {
			logger.Errorw("HTTPS server shutdown error", "error", err)
		}

		// Close database connections
		sqlDB, err := db.DB()
		if err != nil {
			logger.Errorw("Error getting DB instance", "error", err)
		} else {
			if err := sqlDB.Close(); err != nil {
				logger.Errorw("Error closing database", "error", err)
			}
		}

		logger.Info("Graceful shutdown completed")
		close(abort)
	}()

	logger.Debug("Waiting for abort signal")
	<-abort
}

func interactiveAccountRecovery(repositories *app.Repositories, utils *app.Utilities) {
	// check if we are in the same folder as the binary is in
	ex, err := os.Executable()
	if err != nil {
		_, _ = fmt.Printf("Error getting executable path: %s\n", err)
		return
	}
	binPath := filepath.Dir(ex)
	currentPath, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Printf("Error getting current directory: %s\n", err)
		return
	}
	if binPath != currentPath {
		_, _ = fmt.Printf("Please run this command from the same directory as the binary (%s)\n", binPath)
		return
	}

	// get the username to recover
	var user *model.User
	for user == nil {
		account := ""
		_, _ = fmt.Print("Enter account username: ")
		_, _ = fmt.Scanln(&account)
		username, err := vo.NewUsername(account)
		if err != nil {
			_, _ = fmt.Println("Invalid username")
			continue
		}
		user, err = repositories.User.GetByUsername(
			context.TODO(),
			username,
			&repository.UserOption{},
		)
		if err != nil {
			_, _ = fmt.Printf("Could not find username: %s\n", err)
			continue
		}
		_, _ = fmt.Println("User found")
	}
	uid := user.ID.MustGet()
	for {
		passwordInput := ""
		passwordConfirmInput := ""
		_, _ = fmt.Print("New password: ")
		_, _ = fmt.Scanln(&passwordInput)
		_, _ = fmt.Print("Confirm password: ")
		_, _ = fmt.Scanln(&passwordConfirmInput)
		if passwordInput != passwordConfirmInput {
			_, _ = fmt.Println("Repeated password does not match")
			continue
		}
		newPassword, err := vo.NewReasonableLengthPassword(passwordInput)
		if err != nil {
			_, _ = fmt.Printf("Error in password: %s\n", err)
			continue
		}
		hash, err := utils.PasswordHasher.Hash(newPassword.String())
		if err != nil {
			_, _ = fmt.Printf("Failed to hash password: %s\n", err)
			continue
		}
		err = repositories.User.UpdatePasswordHashByID(
			context.TODO(),
			&uid,
			hash,
		)
		if err != nil {
			_, _ = fmt.Printf("Failed to update password: %s\n", err)
			continue
		}
		_, _ = fmt.Println("Password updated")
		break
	}
	// remove any SSO or TOTP related data
	user.SSOID = nullable.NewNullableWithValue("")
	err = repositories.User.RemoveTOTP(context.TODO(), &uid)
	if err != nil {
		_, _ = fmt.Println("Failed to remove TOTP:", err)
	}
	err = repositories.User.UpdateUserToNoSSO(context.TODO(), &uid)
	if err != nil {
		_, _ = fmt.Println("Failed to remove TOTP:", err)
	}
}

func setLogLevels(db *gorm.DB, atomicLogger *zap.AtomicLevel, forceDebug bool) error {
	// if debug flag is set, force debug logging and ignore db settings
	if forceDebug {
		db.Logger = db.Logger.LogMode(gormLogger.Info)
		atomicLogger.SetLevel(zap.DebugLevel)
		return nil
	}

	// set log levels from DB for logger and db logger
	var dbLogLevel database.Option
	res := db.
		Where("key = ?", data.OptionKeyDBLogLevel).
		First(&dbLogLevel)

	if res.Error != nil && !errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to get DB log level: %w", res.Error)
	}
	switch dbLogLevel.Value {
	case "silent":
		db.Logger = db.Logger.LogMode(gormLogger.Silent)
	case "info":
		db.Logger = db.Logger.LogMode(gormLogger.Info)
	case "warn":
		db.Logger = db.Logger.LogMode(gormLogger.Warn)
	case "error":
		db.Logger = db.Logger.LogMode(gormLogger.Error)
	}
	var logLevel database.Option
	res = db.
		Where("key = ?", data.OptionKeyLogLevel).
		First(&logLevel)
	if res.Error != nil && !errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to get log level: %w", res.Error)
	}
	switch logLevel.Value {
	case "debug":
		atomicLogger.SetLevel(zap.DebugLevel)
	case "info":
		atomicLogger.SetLevel(zap.InfoLevel)
	case "warn":
		atomicLogger.SetLevel(zap.WarnLevel)
	case "error":
		atomicLogger.SetLevel(zap.ErrorLevel)
	}
	return nil
}
