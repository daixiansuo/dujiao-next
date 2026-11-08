package application

import (
	"context"
	"sync"
	"time"

	"github.com/dujiao-next/internal/modules/backup/contract"
	"github.com/dujiao-next/internal/modules/backup/domain"
	settingscontract "github.com/dujiao-next/internal/modules/settings/contract"
	"github.com/robfig/cron/v3"
)

type Options struct {
	Driver    string
	SecretKey string
	TempDir   string
	Timeout   time.Duration
}

// Service owns the lifetime of backup jobs. Request cancellation never owns a job.
type Service struct {
	records  contract.RecordStore
	settings settingscontract.Store
	dumper   contract.Dumper
	objects  contract.ObjectStoreFactory
	options  Options
	// ops serializes configuration changes, job admission and object deletion.
	ops          sync.Mutex
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	current      *domain.Record
	jobs         sync.WaitGroup
	scheduler    *cron.Cron
	scheduleID   cron.EntryID
	scheduleSpec string
	runtimeError string
	cleanupError string
	reload       chan struct{}
}

func NewService(records contract.RecordStore, settings settingscontract.Store, dumper contract.Dumper, objects contract.ObjectStoreFactory, options Options) *Service {
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Minute
	}
	return &Service{records: records, settings: settings, dumper: dumper, objects: objects, options: options, reload: make(chan struct{}, 1)}
}

func copyRecord(record *domain.Record) *domain.Record {
	if record == nil {
		return nil
	}
	result := *record
	if record.FinishedAt != nil {
		t := *record.FinishedAt
		result.FinishedAt = &t
	}
	return &result
}

func (s *Service) supported() bool {
	return s.options.Driver == "postgres" || s.options.Driver == "postgresql"
}

func (s *Service) setCurrent(record *domain.Record) {
	s.mu.Lock()
	s.current = copyRecord(record)
	s.mu.Unlock()
}

func (s *Service) shortContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := min(s.options.Timeout, 10*time.Second)
	return context.WithTimeout(ctx, timeout)
}

// lockOps lets lifecycle work stop while an HTTP storage operation still owns ops.
func (s *Service) lockOps(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if s.ops.TryLock() {
		return true
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
			if s.ops.TryLock() {
				return true
			}
		}
	}
}

func (s *Service) finalContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), min(s.options.Timeout, 2*time.Second))
}
