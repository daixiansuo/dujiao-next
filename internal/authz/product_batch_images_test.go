package authz

import "testing"

func TestBuiltinRolesAuthorizeProductBatchImages(t *testing.T) {
	svc := setupAuthzServiceTest(t)
	if err := svc.BootstrapBuiltinRoles(); err != nil {
		t.Fatalf("bootstrap builtin roles failed: %v", err)
	}
	for i, tc := range []struct {
		role string
		want bool
	}{
		{role: "operations", want: true},
		{role: "readonly_auditor", want: false},
		{role: "support", want: false},
		{role: "integration", want: false},
		{role: "finance", want: false},
		{role: "system_admin", want: false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			adminID := uint(i + 1)
			if err := svc.SetAdminRoles(adminID, []string{tc.role}); err != nil {
				t.Fatalf("assign %s role failed: %v", tc.role, err)
			}
			allow, err := svc.EnforceAdmin(adminID, "/api/v1/admin/products/batch-images", "POST")
			if err != nil {
				t.Fatalf("enforce batch images for %s failed: %v", tc.role, err)
			}
			if allow != tc.want {
				t.Fatalf("batch images for %s allow=%v, want %v", tc.role, allow, tc.want)
			}
		})
	}
}
