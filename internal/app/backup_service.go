package app

import (
	"context"

	backupapp "github.com/dujiao-next/internal/modules/backup/application"
)

// backupService runs with the HTTP process and owns no queue resources.
type backupService struct{ service *backupapp.Service }

func (*backupService) Name() string                      { return "database-backup" }
func (s *backupService) Start(ctx context.Context) error { return s.service.Run(ctx) }
func (s *backupService) Stop(ctx context.Context) error  { return s.service.Stop(ctx) }
