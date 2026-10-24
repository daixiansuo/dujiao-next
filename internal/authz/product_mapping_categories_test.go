package authz

import "testing"

func TestIntegrationCanReadProductMappingCategories(t *testing.T) {
	svc := setupAuthzServiceTest(t)
	if err := svc.BootstrapBuiltinRoles(); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetAdminRoles(1, []string{"integration"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		method string
		want   bool
	}{
		{path: "/api/v1/admin/categories", method: "GET", want: true},
		{path: "/api/v1/admin/categories", method: "POST", want: false},
		{path: "/api/v1/admin/categories/1", method: "PUT", want: false},
		{path: "/api/v1/admin/categories/1", method: "DELETE", want: false},
	} {
		allow, err := svc.EnforceAdmin(1, tc.path, tc.method)
		if err != nil || allow != tc.want {
			t.Fatalf("%s %s: allow=%v err=%v, want %v", tc.method, tc.path, allow, err, tc.want)
		}
	}
}
