package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"gorm.io/gorm"

	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/validate"
)

// BackupFile represents a backup file available for download
type BackupFile struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"createdAt"`
	RelativePath string    `json:"relativePath"`
}

type Backup struct {
	Common
	OptionService *Option
	DB            *gorm.DB
	FilePath      string // base file path for application data
}

// BackupStatus represents the status of a backup operation
type BackupStatus struct {
	IsRunning    bool      `json:"isRunning"`
	IsComplete   bool      `json:"isComplete"`
	HasError     bool      `json:"hasError"`
	ErrorMessage string    `json:"errorMessage"`
	StartTime    time.Time `json:"startTime"`
	EndTime      time.Time `json:"endTime"`
	BackupPath   string    `json:"backupPath"`
	Progress     string    `json:"progress"`
}

// BackupResult represents the result of a backup operation
type BackupResult struct {
	BackupPath   string        `json:"backupPath"`
	DatabaseSize int64         `json:"databaseSize"`
	FilesSize    int64         `json:"filesSize"`
	TotalSize    int64         `json:"totalSize"`
	Duration     time.Duration `json:"duration"`
}

var (
	currentBackupStatus *BackupStatus
)

// CreateBackup creates a backup of the database and files
func (b *Backup) CreateBackup(
	ctx context.Context,
	session *model.Session,
) error {
	ae := NewAuditEvent("Backup.CreateBackup", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		b.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return errors.New("unauthorized")
	}

	// check if backup is already running
	if currentBackupStatus != nil && currentBackupStatus.IsRunning {
		return errors.New("backup already in progress")
	}

	// initialize backup status
	currentBackupStatus = &BackupStatus{
		IsRunning:  true,
		IsComplete: false,
		HasError:   false,
		StartTime:  time.Now(),
		Progress:   "starting backup",
	}

	// run backup synchronously to lock interface
	err = b.performBackup(ctx)
	currentBackupStatus.IsRunning = false
	currentBackupStatus.EndTime = time.Now()

	if err != nil {
		currentBackupStatus.HasError = true
		currentBackupStatus.ErrorMessage = err.Error()
		b.Logger.Errorw("backup failed", "error", err)
		b.AuditLogAuthorized(ae)
		return errs.Wrap(err)
	} else {
		currentBackupStatus.IsComplete = true
		currentBackupStatus.Progress = "backup completed"
		b.Logger.Infow("backup completed successfully", "path", currentBackupStatus.BackupPath)

		// automatically cleanup old backups to maintain maximum of 3
		currentBackupStatus.Progress = "cleaning up old backups"
		cleanupErr := b.CleanupOldBackups(ctx, session, 3)
		if cleanupErr != nil {
			b.Logger.Warnw("failed to cleanup old backups", "error", cleanupErr)
			// don't fail the backup operation if cleanup fails
		} else {
			b.Logger.Debugw("cleaned up old backups, keeping latest 3")
		}
		ae.Details["backupPath"] = currentBackupStatus.BackupPath
	}

	if currentBackupStatus.HasError {
		ae.Details["error"] = currentBackupStatus.ErrorMessage
		b.AuditLogAuthorized(ae)
		return errs.Wrap(errors.New(currentBackupStatus.ErrorMessage))
	}

	b.AuditLogAuthorized(ae)
	return nil
}

// performBackup performs the actual backup operation
func (b *Backup) performBackup(ctx context.Context) error {
	timestamp := time.Now().Format("20060102-150405")
	filesPath := strings.TrimSuffix(b.FilePath, "/")
	backupDir := filepath.Join(filesPath, "backups", fmt.Sprintf("backup-%s", timestamp))

	// create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return errs.Wrap(err)
	}

	currentBackupStatus.Progress = "backing up database"
	b.Logger.Debugw("starting database backup")

	// backup database directly to backup root
	if err := b.backupDatabase(ctx, backupDir); err != nil {
		return errs.Wrap(err)
	}

	currentBackupStatus.Progress = "backing up files"
	b.Logger.Debugw("starting files backup")

	// backup files directly to backup root (preserving directory structure)
	if err := b.backupFiles(backupDir); err != nil {
		return errs.Wrap(err)
	}

	currentBackupStatus.Progress = "compressing backup"
	b.Logger.Debugw("compressing backup")

	// compress backup
	backupArchive := backupDir + ".tar.gz"
	if err := b.compressBackup(backupDir, backupArchive); err != nil {
		return errs.Wrap(err)
	}

	// remove uncompressed backup directory
	if err := os.RemoveAll(backupDir); err != nil {
		b.Logger.Warnw("failed to remove uncompressed backup directory", "error", err)
	}

	currentBackupStatus.BackupPath = backupArchive
	return nil
}

// backupDatabase creates a backup of the sqlite database
func (b *Backup) backupDatabase(ctx context.Context, backupPath string) error {
	// get the underlying sql.DB
	sqlDB, err := b.DB.DB()
	if err != nil {
		return errs.Wrap(err)
	}

	// execute wal checkpoint to ensure all data is written to main db file
	_, err = sqlDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		b.Logger.Warnw("failed to checkpoint wal", "error", err)
		// continue anyway as this is not critical
	}

	// extract database path from DSN
	dbPath := b.extractDatabasePath()

	// copy main database file
	if err := b.copyFile(dbPath, filepath.Join(backupPath, "db.sqlite3")); err != nil {
		return errs.Wrap(err)
	}

	// copy wal file if it exists
	walPath := dbPath + "-wal"
	if _, err := os.Stat(walPath); err == nil {
		if err := b.copyFile(walPath, filepath.Join(backupPath, "db.sqlite3-wal")); err != nil {
			b.Logger.Debugw("failed to copy wal file", "error", err)
		}
	}

	// copy shm file if it exists
	shmPath := dbPath + "-shm"
	if _, err := os.Stat(shmPath); err == nil {
		if err := b.copyFile(shmPath, filepath.Join(backupPath, "db.sqlite3-shm")); err != nil {
			b.Logger.Debugw("failed to copy shm file", "error", err)
		}
	}

	return nil
}

// backupFiles creates a backup of application files
func (b *Backup) backupFiles(backupPath string) error {
	// files are stored in the path specified by --files flag
	// remove trailing slash if present for consistent path joining
	filesPath := strings.TrimSuffix(b.FilePath, "/")

	filesToBackup := []string{"assets", "attachments", "certs"}

	for _, dir := range filesToBackup {
		srcPath := filepath.Join(filesPath, dir)
		dstPath := filepath.Join(backupPath, dir)

		// check if source directory exists
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			b.Logger.Debugw("directory does not exist, skipping", "path", srcPath)
			continue
		}

		// copy directory
		if err := b.copyDir(srcPath, dstPath); err != nil {
			return errs.Wrap(err)
		}
	}

	return nil
}

// extractDatabasePath extracts the database file path from the GORM DSN
func (b *Backup) extractDatabasePath() string {
	// get the underlying sql.DB to access the data source name
	sqlDB, err := b.DB.DB()
	if err != nil {
		b.Logger.Debugw("failed to get sql.DB, using default path", "error", err)
		return "./db.sqlite3"
	}

	// try to get database list to find the actual file path
	rows, err := sqlDB.Query("PRAGMA database_list")
	if err != nil {
		b.Logger.Debugw("failed to query database list, using default path", "error", err)
		return "./db.sqlite3"
	}
	defer rows.Close()

	for rows.Next() {
		var seq int
		var name, file string
		err := rows.Scan(&seq, &name, &file)
		if err != nil {
			continue
		}
		// main database has seq=0 and name="main"
		if seq == 0 && name == "main" && file != "" {
			b.Logger.Debugw("found database path from PRAGMA database_list", "path", file)
			return file
		}
	}

	// fallback to default
	b.Logger.Debugw("could not determine database path from PRAGMA, using default")
	return "./db.sqlite3"
}

// compressBackup compresses the backup directory into a tar.gz file
func (b *Backup) compressBackup(srcDir, dstFile string) error {
	file, err := os.Create(dstFile)
	if err != nil {
		return errs.Wrap(err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// get relative path
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		// write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// write file content if it's a regular file
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tarWriter, file)
			return err
		}

		return nil
	})
}

// copyFile copies a file from src to dst
func (b *Backup) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return errs.Wrap(err)
	}
	defer sourceFile.Close()

	// create destination directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return errs.Wrap(err)
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return errs.Wrap(err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return errs.Wrap(err)
}

// copyDir recursively copies a directory from src to dst
func (b *Backup) copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return b.copyFile(path, dstPath)
	})
}

// CleanupOldBackups removes old backup files to save disk space
func (b *Backup) CleanupOldBackups(
	ctx context.Context,
	session *model.Session,
	keepCount int,
) error {
	ae := NewAuditEvent("Backup.CleanupOldBackups", session)
	ae.Details["keepCount"] = keepCount

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		b.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	filesPath := strings.TrimSuffix(b.FilePath, "/")
	backupDir := filepath.Join(filesPath, "backups")
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil // no backups directory
	}

	// get all backup files
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return errs.Wrap(err)
	}

	// filter backup files and sort by modification time
	var backupFiles []os.FileInfo
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "backup-") && strings.HasSuffix(file.Name(), ".tar.gz") {
			info, err := file.Info()
			if err != nil {
				continue
			}
			backupFiles = append(backupFiles, info)
		}
	}

	// if we have more backups than we want to keep, delete the oldest ones
	if len(backupFiles) > keepCount {
		// sort by modification time (oldest first)
		for i := 0; i < len(backupFiles)-1; i++ {
			for j := i + 1; j < len(backupFiles); j++ {
				if backupFiles[i].ModTime().After(backupFiles[j].ModTime()) {
					backupFiles[i], backupFiles[j] = backupFiles[j], backupFiles[i]
				}
			}
		}

		// delete oldest files
		filesToDelete := len(backupFiles) - keepCount
		deletedFiles := []string{}
		for i := 0; i < filesToDelete; i++ {
			filePath := filepath.Join(backupDir, backupFiles[i].Name())
			if err := os.Remove(filePath); err != nil {
				b.Logger.Warnw("failed to delete old backup", "file", filePath, "error", err)
			} else {
				b.Logger.Debugw("deleted old backup", "file", filePath)
				deletedFiles = append(deletedFiles, backupFiles[i].Name())
			}
		}
		ae.Details["deletedFiles"] = deletedFiles
		ae.Details["deletedCount"] = len(deletedFiles)
	}

	ae.Details["totalBackups"] = len(backupFiles)
	b.AuditLogAuthorized(ae)
	return nil
}

// ListBackups returns a list of available backup files
func (b *Backup) ListBackups(
	ctx context.Context,
	session *model.Session,
) ([]BackupFile, error) {
	ae := NewAuditEvent("Backup.ListBackups", session)

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		b.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	filesPath := strings.TrimSuffix(b.FilePath, "/")
	backupDir := filepath.Join(filesPath, "backups")

	// check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []BackupFile{}, nil // return empty list if no backups directory
	}

	// read backup directory
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	var backupFiles []BackupFile
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "backup-") && strings.HasSuffix(file.Name(), ".tar.gz") {
			info, err := file.Info()
			if err != nil {
				continue
			}

			backupFiles = append(backupFiles, BackupFile{
				Name:         file.Name(),
				Size:         info.Size(),
				CreatedAt:    info.ModTime(),
				RelativePath: filepath.Join("backups", file.Name()),
			})
		}
	}

	// sort by creation time (newest first)
	for i := 0; i < len(backupFiles)-1; i++ {
		for j := i + 1; j < len(backupFiles); j++ {
			if backupFiles[i].CreatedAt.Before(backupFiles[j].CreatedAt) {
				backupFiles[i], backupFiles[j] = backupFiles[j], backupFiles[i]
			}
		}
	}

	ae.Details["backupCount"] = len(backupFiles)
	b.AuditLogAuthorized(ae)
	return backupFiles, nil
}

// GetBackupFile returns a file handle to a backup file if it exists and is valid
func (b *Backup) GetBackupFile(
	ctx context.Context,
	session *model.Session,
	filename string,
) (*os.File, error) {
	ae := NewAuditEvent("Backup.DownloadBackup", session)
	ae.Details["filename"] = filename

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		b.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	// validate filename - must be a backup file
	if !strings.HasPrefix(filename, "backup-") || !strings.HasSuffix(filename, ".tar.gz") {
		b.Logger.Debugw("invalid backup filename format", "filename", filename)
		return nil, validate.WrapErrorWithField(errors.New("invalid backup filename"), "filename")
	}

	// get backup directory path
	filesPath := strings.TrimSuffix(b.FilePath, "/")
	backupDir := filepath.Join(filesPath, "backups")

	// use os.OpenRoot for secure file access within backup directory
	root, err := os.OpenRoot(backupDir)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer root.Close()

	// try to stat the file using the secure root - this prevents directory traversal
	info, err := root.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			b.Logger.Debugw("backup file not found", "filename", filename)
			return nil, gorm.ErrRecordNotFound
		}
		return nil, errs.Wrap(err)
	}

	if !info.Mode().IsRegular() {
		b.Logger.Debugw("requested file is not a regular file", "filename", filename)
		return nil, validate.WrapErrorWithField(errors.New("not a regular file"), "filename")
	}

	// open the file using the secure root - this maintains security throughout
	file, err := root.Open(filename)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	ae.Details["backupSize"] = info.Size()
	b.AuditLogAuthorized(ae)
	return file, nil
}
