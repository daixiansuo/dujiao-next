package container

import (
	"time"

	backupapp "github.com/dujiao-next/internal/modules/backup/application"
	backupstore "github.com/dujiao-next/internal/modules/backup/infrastructure/gormstore"
	"github.com/dujiao-next/internal/modules/backup/infrastructure/pgdump"
	"github.com/dujiao-next/internal/modules/backup/infrastructure/r2"
	"github.com/dujiao-next/internal/platform/database/gormdb"
)

func (c *Container) initBackupService() {
	c.BackupService = backupapp.NewService(
		backupstore.New(gormdb.DB), c.SettingRepo,
		pgdump.New(c.Config.Database.DSN, c.Config.Backup.PgDumpPath), r2.New,
		backupapp.Options{Driver: c.Config.Database.Driver, SecretKey: c.Config.App.SecretKey,
			TempDir: c.Config.Backup.TempDir, Timeout: time.Duration(c.Config.Backup.TimeoutSeconds) * time.Second},
	)
}
