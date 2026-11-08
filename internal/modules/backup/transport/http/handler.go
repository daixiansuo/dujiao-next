package backuphttp

import (
	"errors"
	"github.com/dujiao-next/internal/i18n"

	backupapp "github.com/dujiao-next/internal/modules/backup/application"
	"github.com/dujiao-next/internal/modules/backup/domain"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *backupapp.Service }

func NewHandler(service *backupapp.Service) *Handler { return &Handler{service: service} }

func respondError(c *gin.Context, err error) {
	key, code := "error.backup_failed", response.CodeInternal
	switch {
	case errors.Is(err, domain.ErrBusy):
		key, code = "error.backup_busy", response.CodeBadRequest
	case errors.Is(err, domain.ErrNotFound):
		key, code = "error.backup_not_found", response.CodeNotFound
	case errors.Is(err, domain.ErrUnsupported):
		key, code = "error.backup_unsupported", response.CodeBadRequest
	case errors.Is(err, domain.ErrInvalidConfig):
		key, code = "error.backup_invalid_config", response.CodeBadRequest
	case errors.Is(err, domain.ErrTargetChange):
		key, code = "error.backup_target_change", response.CodeBadRequest
	case errors.Is(err, domain.ErrNotReady):
		key, code = "error.backup_not_ready", response.CodeBadRequest
	}
	// Object clients and subprocess errors can carry credentials. The fixed i18n
	// diagnosis is sufficient here; do not send their raw text to handler logging.
	ginutil.RespondError(c, code, key, nil)
}

func (h *Handler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig()
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *Handler) UpdateConfig(c *gin.Context) {
	var cfg domain.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		respondError(c, domain.ErrInvalidConfig)
		return
	}
	cfg, err := h.service.UpdateConfig(cfg)
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *Handler) Test(c *gin.Context) {
	if err := h.service.Test(c.Request.Context()); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil)
}

func (h *Handler) Status(c *gin.Context) {
	status, err := h.service.Status(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}
	locale := i18n.ResolveLocale(c)
	status.Error = localized(locale, status.Error)
	status.CleanupError = localized(locale, status.CleanupError)
	if status.Current != nil {
		status.Current.Error = localized(locale, status.Current.Error)
	}
	if status.LastSuccess != nil {
		status.LastSuccess.Error = localized(locale, status.LastSuccess.Error)
	}
	response.Success(c, status)
}

func (h *Handler) List(c *gin.Context) {
	page, size := ginutil.ParsePagination(c)
	records, count, err := h.service.List(c.Request.Context(), size, (page-1)*size)
	if err != nil {
		respondError(c, err)
		return
	}
	if records == nil {
		records = []domain.Record{}
	}
	locale := i18n.ResolveLocale(c)
	for i := range records {
		records[i].Error = localized(locale, records[i].Error)
	}
	response.Success(c, gin.H{"items": records, "total": count, "page": page, "page_size": size})
}

func (h *Handler) Start(c *gin.Context) {
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}
	record, err := h.service.StartBackup("manual", adminID)
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, record)
}

func (h *Handler) Download(c *gin.Context) {
	url, err := h.service.Download(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, gin.H{"url": url})
}

func (h *Handler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		respondError(c, err)
		return
	}
	response.Success(c, nil)
}

func localized(locale, key string) string {
	if key == "" {
		return ""
	}
	// Compatibility for records created before diagnoses were persisted as keys.
	switch key {
	case "database backup tool unavailable", "upload outcome pending verification":
		key = "error.backup_not_ready"
	case "cannot create backup temporary directory", "database export failed", "database backup canceled",
		"cannot open exported database", "invalid database export", "cannot hash exported database",
		"cannot read exported database", "cannot persist backup upload metadata",
		"uploaded database verification failed", "uploaded database object not found",
		"database backup interrupted during export":
		key = "error.backup_failed"
	}
	return i18n.T(locale, key)
}
