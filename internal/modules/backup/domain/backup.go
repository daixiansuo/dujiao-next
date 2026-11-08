package domain

import (
	"errors"
	"time"
)

const (
	SettingKey      = "database_backup_config"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StageExporting  = "exporting"
	StageUploading  = "uploading"
)

var (
	ErrBusy          = errors.New("backup already running")
	ErrNotFound      = errors.New("backup not found")
	ErrUnsupported   = errors.New("backup requires PostgreSQL")
	ErrInvalidConfig = errors.New("invalid backup configuration")
	ErrTargetChange  = errors.New("existing backups use a different storage target")
	ErrNotReady      = errors.New("backup is not ready")
)

// Config is encrypted at rest and always masked before crossing the HTTP boundary.
type Config struct {
	Endpoint         string `json:"endpoint"`
	Bucket           string `json:"bucket"`
	AccessKeyID      string `json:"access_key_id"`
	SecretAccessKey  string `json:"secret_access_key,omitempty"`
	SecretConfigured bool   `json:"secret_configured,omitempty"`
	Prefix           string `json:"prefix"`
	Enabled          bool   `json:"enabled"`
	DailyTime        string `json:"daily_time"`
	Timezone         string `json:"timezone"`
	RetainCount      int    `json:"retain_count"`
}

func DefaultConfig() Config {
	return Config{Prefix: "dujiao-next", DailyTime: "03:00", Timezone: "Asia/Shanghai", RetainCount: 14}
}

type Record struct {
	ID         string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Source     string     `gorm:"type:varchar(16);not null" json:"source"`
	AdminID    uint       `json:"admin_id"`
	Status     string     `gorm:"type:varchar(16);not null;index" json:"status"`
	Stage      string     `gorm:"type:varchar(16)" json:"stage"`
	Filename   string     `json:"filename"`
	Size       int64      `json:"size"`
	SHA256     string     `gorm:"type:varchar(64)" json:"sha256"`
	Endpoint   string     `json:"endpoint"`
	Bucket     string     `json:"bucket"`
	ObjectKey  string     `json:"object_key"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `gorm:"index" json:"created_at"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (Record) TableName() string { return "database_backups" }

type ToolInfo struct {
	PgDumpVersion string `json:"pg_dump_version"`
	ServerVersion string `json:"server_version"`
}

type RuntimeStatus struct {
	Supported bool `json:"supported"`
	Ready     bool `json:"ready"`
	ToolInfo
	Error        string     `json:"error"`
	Current      *Record    `json:"current,omitempty"`
	NextRun      *time.Time `json:"next_run,omitempty"`
	LastSuccess  *Record    `json:"last_success,omitempty"`
	CleanupError string     `json:"cleanup_error,omitempty"`
}

type ObjectInfo struct {
	Size   int64
	SHA256 string
}
