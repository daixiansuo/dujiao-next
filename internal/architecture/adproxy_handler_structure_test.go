package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdProxyFeatureStaysRemoved(t *testing.T) {
	repositoryRoot := findRepositoryRoot(t)
	for _, relativePath := range []string{
		"internal/modules/adproxy",
		"internal/adgateway",
		"internal/http/handlers/admin/admin_ad_proxy.go",
		"internal/service/ad_proxy_service.go",
	} {
		path := filepath.Join(repositoryRoot, filepath.FromSlash(relativePath))
		if _, err := os.Stat(path); err == nil {
			t.Errorf("removed advertising code must stay absent: %s", relativePath)
		} else if !os.IsNotExist(err) {
			t.Errorf("stat removed advertising code %s: %v", relativePath, err)
		}
	}
}
