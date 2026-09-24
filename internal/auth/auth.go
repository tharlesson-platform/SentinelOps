package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	Role         string `json:"role"`
	Organization string `json:"organization"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret       []byte
	user         string
	passwordHash string
}

type Authenticator interface {
	ParseAuthorization(context.Context, string) (Claims, error)
}

func New(secret, user, passwordHash string) *Manager {
	return &Manager{secret: []byte(secret), user: user, passwordHash: passwordHash}
}

func (m *Manager) Login(user, password string) (string, error) {
	if user != m.user || bcrypt.CompareHashAndPassword([]byte(m.passwordHash), []byte(password)) != nil {
		return "", errors.New("invalid credentials")
	}
	now := time.Now().UTC()
	claims := Claims{Role: "Platform Administrator", Organization: "local", RegisteredClaims: jwt.RegisteredClaims{
		Subject: user, Issuer: "sentinelops-local", Audience: []string{"sentinelops"},
		IssuedAt: jwt.NewNumericDate(now), ID: fmt.Sprintf("%d", now.UnixNano()),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *Manager) ParseAuthorization(_ context.Context, value string) (Claims, error) {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Claims{}, errors.New("missing bearer token")
	}
	token, err := jwt.ParseWithClaims(parts[1], &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	}, jwt.WithAudience("sentinelops"), jwt.WithIssuer("sentinelops-local"))
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid or expired token")
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return Claims{}, errors.New("invalid claims")
	}
	return *claims, nil
}

type OIDCAuthenticator struct {
	verifiers     []*oidc.IDTokenVerifier
	requiredScope string
	requiredGroup string
	organization  string
}

func NewOIDC(ctx context.Context, issuer, audience, requiredScope, requiredGroup, organization string) (*OIDCAuthenticator, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	verifiers := []*oidc.IDTokenVerifier{provider.Verifier(&oidc.Config{ClientID: audience})}

	// Entra can issue a v1 access token for an API whose authority is v2.0.
	// v1 tokens use the tenant issuer without /v2.0 and the API App ID URI as
	// audience, while v2 tokens use the client ID. Accept both token formats.
	legacyIssuer := strings.TrimSuffix(issuer, "/v2.0")
	if legacyIssuer != issuer {
		legacyProvider, legacyErr := oidc.NewProvider(ctx, legacyIssuer)
		if legacyErr == nil {
			verifiers = append(verifiers,
				legacyProvider.Verifier(&oidc.Config{ClientID: "api://" + audience}),
				legacyProvider.Verifier(&oidc.Config{ClientID: "api://" + audience + "/" + requiredScope}),
			)
		}
	}
	return &OIDCAuthenticator{verifiers: verifiers, requiredScope: requiredScope, requiredGroup: requiredGroup, organization: organization}, nil
}
func (o *OIDCAuthenticator) ParseAuthorization(ctx context.Context, value string) (Claims, error) {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Claims{}, errors.New("missing bearer token")
	}
	var token *oidc.IDToken
	var err error
	for _, verifier := range o.verifiers {
		token, err = verifier.Verify(ctx, parts[1])
		if err == nil {
			break
		}
	}
	if err != nil || token == nil {
		return Claims{}, errors.New("invalid or expired OIDC token")
	}
	var raw struct {
		Roles       []string `json:"sentinelops_roles"`
		Groups      []string `json:"groups"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
		Organization string   `json:"organization"`
		Scope        string   `json:"scope"`
		Scopes       []string `json:"scp"`
	}
	if err := token.Claims(&raw); err != nil {
		return Claims{}, err
	}
	if !hasScope(raw.Scope, raw.Scopes, o.requiredScope) {
		return Claims{}, errors.New("OIDC token lacks required API scope")
	}
	if !hasGroup(raw.Groups, o.requiredGroup) {
		return Claims{}, errors.New("OIDC token lacks required group")
	}
	role := selectRole(append(append(raw.Roles, raw.Groups...), raw.RealmAccess.Roles...))
	if role == "" {
		role = "Viewer"
	}
	if token.Subject == "" {
		return Claims{}, errors.New("OIDC token requires stable sub claim")
	}
	if raw.Organization == "" {
		raw.Organization = o.organization
	}
	if raw.Organization == "" {
		return Claims{}, errors.New("OIDC token requires organization mapping")
	}
	return Claims{Role: role, Organization: raw.Organization, RegisteredClaims: jwt.RegisteredClaims{Subject: token.Subject, Issuer: token.Issuer, Audience: token.Audience, ExpiresAt: jwt.NewNumericDate(token.Expiry)}}, nil
}

func hasGroup(groups []string, wanted string) bool {
	if wanted == "" {
		return true
	}
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group), wanted) {
			return true
		}
	}
	return false
}
func hasScope(scope string, scopes []string, wanted string) bool {
	if wanted == "" {
		return false
	}
	for _, candidate := range append(strings.Fields(scope), scopes...) {
		if candidate == wanted {
			return true
		}
	}
	return false
}

func selectRole(values []string) string {
	order := []string{"Platform Administrator", "SRE Administrator", "SRE Operator", "Application Owner", "Developer", "Auditor", "Viewer"}
	for _, wanted := range order {
		for _, actual := range values {
			normalized := strings.TrimPrefix(actual, "sentinelops:")
			if strings.EqualFold(normalized, wanted) {
				return wanted
			}
		}
	}
	return ""
}

func Can(role, permission string) bool {
	// Requests that can lead to data export or erasure remain restricted even
	// for otherwise read-only audit roles. The Platform Administrator is the
	// sole role allowed to create, inspect or approve this control plane flow.
	if strings.HasPrefix(permission, "data-lifecycle:") {
		return role == "Platform Administrator"
	}
	if role == "Platform Administrator" {
		return true
	}
	grants := map[string][]string{
		"SRE Administrator": {"service:", "asset:", "scenario:", "validation:", "agent:", "release:", "incident:", "alert-route:", "telemetry:read"},
		"SRE Operator":      {"service:read", "asset:", "scenario:", "validation:", "agent:read", "release:", "incident:", "alert-route:", "telemetry:read"},
		"Developer":         {"service:read", "asset:read", "scenario:read", "validation:read", "release:create", "telemetry:read"},
		"Application Owner": {"service:read", "asset:read", "scenario:", "validation:", "release:", "incident:read", "telemetry:read"},
		"Auditor":           {":read"}, "Viewer": {"service:read", "asset:read", "scenario:read", "validation:read", "telemetry:read"},
	}
	for _, g := range grants[role] {
		if strings.HasSuffix(g, ":") && strings.HasPrefix(permission, g) || strings.HasPrefix(g, ":") && strings.HasSuffix(permission, g) || g == permission {
			return true
		}
	}
	return false
}
