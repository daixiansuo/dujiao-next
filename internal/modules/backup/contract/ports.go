package contract

import (
	"context"
	"os"
	"time"

	"github.com/dujiao-next/internal/modules/backup/domain"
)

type RecordStore interface {
	Create(context.Context, *domain.Record) error
	Save(context.Context, *domain.Record) error
	Get(context.Context, string) (*domain.Record, error)
	List(context.Context, int, int) ([]domain.Record, int64, error)
	Running(context.Context) ([]domain.Record, error)
	Successful(context.Context) ([]domain.Record, error)
	Delete(context.Context, string) error
}

type Dumper interface {
	Check(context.Context) (domain.ToolInfo, error)
	Dump(context.Context, string) (domain.ToolInfo, error)
}

type ObjectStore interface {
	Upload(context.Context, string, *os.File, string) error
	Head(context.Context, string) (domain.ObjectInfo, error)
	Delete(context.Context, string) error
	Presign(context.Context, string, string, time.Duration) (string, error)
}

type ObjectStoreFactory func(domain.Config) (ObjectStore, error)
