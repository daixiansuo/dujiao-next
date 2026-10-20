package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/modules/backup/domain"
	"github.com/robfig/cron/v3"
)

// Run remains alive on unsupported databases, invalid settings and unavailable
// tools. A backup subsystem failure must not stop the HTTP application.
func (s *Service) Run(ctx context.Context) error {
	life, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	scheduler := cron.New()
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		cancel()
		return domain.ErrBusy
	}
	s.cancel, s.done = cancel, done
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.ctx = nil // Stop admitting jobs before waiting for existing jobs.
		s.mu.Unlock()
		<-scheduler.Stop().Done()
		s.jobs.Wait()
		s.mu.Lock()
		s.scheduler, s.scheduleID, s.scheduleSpec = nil, 0, ""
		s.cancel, s.done = nil, nil
		close(done)
		s.mu.Unlock()
	}()
	if !s.lockOps(life) {
		return nil
	}
	s.recoverTemp()
	s.mu.Lock()
	s.ctx, s.scheduler = life, scheduler
	s.mu.Unlock()
	s.ops.Unlock()
	// Retention has its own daily schedule, independent of automatic backups.
	_, _ = scheduler.AddFunc("CRON_TZ=UTC 0 4 * * *", func() {
		cleanupCtx, cancel := context.WithTimeout(life, s.options.Timeout)
		defer cancel()
		s.cleanup(cleanupCtx)
	})
	s.reloadSchedule(life, scheduler)
	scheduler.Start()
	if s.supported() {
		s.reconcile(life)
		cleanupCtx, cleanupCancel := context.WithTimeout(life, s.options.Timeout)
		s.cleanup(cleanupCtx)
		cleanupCancel()
	}
	retry := time.NewTicker(time.Minute)
	defer retry.Stop()
	for {
		select {
		case <-life.Done():
			return nil
		case <-s.reload:
			s.reloadSchedule(life, scheduler)
		case <-retry.C:
			// Also notices out-of-band settings changes without a process restart.
			s.reloadSchedule(life, scheduler)
			if s.supported() {
				s.reconcile(life)
			}
		}
	}
}

func (s *Service) reloadSchedule(ctx context.Context, scheduler *cron.Cron) {
	if !s.lockOps(ctx) {
		return
	}
	defer s.ops.Unlock()
	if !s.supported() {
		return
	}
	cfg, err := s.loadConfig()
	if err == nil {
		err = validateSchedule(cfg.Config)
	}
	if err == nil && cfg.Enabled {
		_, err = s.decrypted(cfg)
	}
	runtimeError := ""
	if err != nil {
		runtimeError = "error.backup_invalid_config"
	}
	spec := ""
	if err == nil && cfg.Enabled {
		spec = cfg.Timezone + " " + cfg.DailyTime
	}
	s.mu.Lock()
	s.runtimeError = runtimeError
	previous := s.scheduleID
	unchanged := s.scheduleSpec == spec
	s.mu.Unlock()
	if unchanged {
		return
	}
	if previous != 0 {
		scheduler.Remove(previous)
	}
	s.mu.Lock()
	s.scheduleID, s.scheduleSpec = 0, ""
	s.mu.Unlock()
	if spec == "" {
		return
	}

	parts := strings.Split(cfg.DailyTime, ":")
	hour, _ := strconv.Atoi(parts[0])
	minute, _ := strconv.Atoi(parts[1])
	expression := fmt.Sprintf("CRON_TZ=%s %d %d * * *", cfg.Timezone, minute, hour)
	id, err := scheduler.AddFunc(expression, func() {
		_, err := s.StartBackup("scheduled", 0)
		if err != nil && !errors.Is(err, domain.ErrBusy) && !errors.Is(err, domain.ErrNotReady) {
			s.mu.Lock()
			s.runtimeError = "error.backup_failed"
			s.mu.Unlock()
		}
	})
	if err != nil {
		s.mu.Lock()
		s.runtimeError = "error.backup_invalid_config"
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.scheduleID, s.scheduleSpec = id, spec
	s.mu.Unlock()
}

func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) Status(ctx context.Context) (domain.RuntimeStatus, error) {
	result := domain.RuntimeStatus{Supported: s.supported()}
	s.mu.Lock()
	result.Current = copyRecord(s.current)
	result.Error, result.CleanupError = s.runtimeError, s.cleanupError
	running := s.ctx != nil && s.ctx.Err() == nil
	if s.scheduler != nil && s.scheduleID != 0 {
		next := s.scheduler.Entry(s.scheduleID).Next
		if !next.IsZero() {
			result.NextRun = &next
		}
	}
	s.mu.Unlock()
	if !result.Supported {
		result.Error = "error.backup_unsupported"
		return result, nil
	}
	if s.records == nil {
		result.Error = "error.backup_not_ready"
		return result, nil
	}
	records, err := s.records.Successful(ctx)
	if err != nil {
		return result, err
	}
	if len(records) > 0 {
		latest := &records[0]
		for i := range records {
			if records[i].CreatedAt.After(latest.CreatedAt) {
				latest = &records[i]
			}
		}
		result.LastSuccess = copyRecord(latest)
	}
	if !s.lockOps(ctx) {
		return result, ctx.Err()
	}
	_, configErr := s.objectConfig()
	s.ops.Unlock()
	if configErr != nil {
		result.Error = "error.backup_invalid_config"
		return result, nil
	}
	if !running || s.dumper == nil || s.objects == nil {
		result.Error = "error.backup_not_ready"
		return result, nil
	}
	checkCtx, cancel := s.shortContext(ctx)
	defer cancel()
	info, err := s.dumper.Check(checkCtx)
	result.ToolInfo = info
	if err != nil {
		result.Error = "error.backup_not_ready"
		return result, nil
	}
	result.Ready, result.Error = true, ""
	return result, nil
}
