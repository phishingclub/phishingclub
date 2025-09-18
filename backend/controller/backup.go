package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/service"
)

type Backup struct {
	Common
	BackupService *service.Backup
}

// CreateBackup starts a backup operation
func (b *Backup) CreateBackup(g *gin.Context) {
	session, _, ok := b.handleSession(g)
	if !ok {
		return
	}

	err := b.BackupService.CreateBackup(g, session)
	if ok := b.handleErrors(g, err); !ok {
		return
	}

	b.Response.OK(g, gin.H{
		"message": "backup started",
	})
}

// ListBackups returns a list of available backup files
func (b *Backup) ListBackups(g *gin.Context) {
	session, _, ok := b.handleSession(g)
	if !ok {
		return
	}

	backups, err := b.BackupService.ListBackups(g, session)
	if ok := b.handleErrors(g, err); !ok {
		return
	}

	b.Response.OK(g, backups)
}

// DownloadBackup serves a backup file for download
func (b *Backup) DownloadBackup(g *gin.Context) {
	session, _, ok := b.handleSession(g)
	if !ok {
		return
	}

	filename := g.Param("filename")
	if filename == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "filename is required"})
		return
	}

	backupFile, err := b.BackupService.GetBackupFile(g, session, filename)
	if ok := b.handleErrors(g, err); !ok {
		return
	}
	defer backupFile.Close()

	// set headers for file download
	g.Header("Content-Description", "File Transfer")
	g.Header("Content-Transfer-Encoding", "binary")
	g.Header("Content-Disposition", "attachment; filename="+filename)
	g.Header("Content-Type", "application/octet-stream")

	// serve the file content directly from the secure file handle
	g.DataFromReader(http.StatusOK, -1, "application/octet-stream", backupFile, nil)
}
