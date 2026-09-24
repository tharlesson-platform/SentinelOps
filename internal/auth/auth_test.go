package auth

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLoginAndParse(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct horse battery staple"), bcrypt.MinCost)
	m := New("01234567890123456789012345678901", "admin", string(hash))
	token, err := m.Login("admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := m.ParseAuthorization(context.Background(), "Bearer "+token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "admin" || !Can(claims.Role, "service:delete") {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestBadPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right"), bcrypt.MinCost)
	if _, err := New("01234567890123456789012345678901", "admin", string(hash)).Login("admin", "wrong"); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestHasScopeAcceptsStandardAndArrayClaims(t *testing.T) {
	for _, test := range []struct {
		name   string
		scope  string
		scopes []string
		want   bool
	}{
		{name: "standard scope claim", scope: "openid profile sentinelops.api", want: true},
		{name: "array scope claim", scopes: []string{"openid", "sentinelops.api"}, want: true},
		{name: "prefix is not a scope", scope: "sentinelops.api.read", want: false},
		{name: "missing", scope: "openid profile", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := hasScope(test.scope, test.scopes, "sentinelops.api"); got != test.want {
				t.Fatalf("hasScope() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestHasGroupIsCaseInsensitiveAndCanBeDisabled(t *testing.T) {
	if !hasGroup([]string{"Infra", "SentinelOps"}, "sentinelops") {
		t.Fatal("expected matching group")
	}
	if hasGroup([]string{"infra"}, "sentinelops") {
		t.Fatal("unexpected group match")
	}
	if !hasGroup(nil, "") {
		t.Fatal("empty group policy should be disabled")
	}
}

func TestAssetPermissionsFollowRBAC(t *testing.T) {
	if !Can("Viewer", "asset:read") || Can("Viewer", "asset:write") {
		t.Fatal("viewer asset permissions are not read-only")
	}
	if !Can("SRE Operator", "asset:write") {
		t.Fatal("SRE operator must reconcile authorized inventory")
	}
}

func TestDataLifecyclePermissionsArePlatformAdminOnly(t *testing.T) {
	for _, role := range []string{"Viewer", "Auditor", "Developer", "Application Owner", "SRE Operator", "SRE Administrator"} {
		if Can(role, "data-lifecycle:read") || Can(role, "data-lifecycle:write") {
			t.Fatalf("%s must not access data lifecycle requests", role)
		}
	}
	if !Can("Platform Administrator", "data-lifecycle:read") || !Can("Platform Administrator", "data-lifecycle:write") {
		t.Fatal("platform administrator must access data lifecycle requests")
	}
}
