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
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)), ID: fmt.Sprintf("%d", now.UnixNano()),
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
	}, jwt.WithAudience("sentinelops"), jwt.WithIssuer("sentinelops-local"), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid or expired token")
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return Claims{}, errors.New("invalid claims")
	}
	return *claims, nil
}

type OIDCAuthenticator struct{ verifier *oidc.IDTokenVerifier }

func NewOIDC(ctx context.Context, issuer, clientID string) (*OIDCAuthenticator, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCAuthenticator{verifier: provider.Verifier(&oidc.Config{ClientID: clientID})}, nil
}
func (o *OIDCAuthenticator) ParseAuthorization(ctx context.Context, value string) (Claims, error) {
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Claims{}, errors.New("missing bearer token")
	}
	token, err := o.verifier.Verify(ctx, parts[1])
	if err != nil {
		return Claims{}, errors.New("invalid or expired OIDC token")
	}
	var raw struct {
		Email       string   `json:"email"`
		Roles       []string `json:"sentinelops_roles"`
		Groups      []string `json:"groups"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
		Organization string `json:"organization"`
	}
	if err := token.Claims(&raw); err != nil {
		return Claims{}, err
	}
	role := selectRole(append(append(raw.Roles, raw.Groups...), raw.RealmAccess.Roles...))
	if role == "" {
		role = "Viewer"
	}
	subject := token.Subject
	if raw.Email != "" {
		subject = raw.Email
	}
	return Claims{Role: role, Organization: raw.Organization, RegisteredClaims: jwt.RegisteredClaims{Subject: subject, Issuer: token.Issuer, Audience: token.Audience, ExpiresAt: jwt.NewNumericDate(token.Expiry)}}, nil
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
	if role == "Platform Administrator" {
		return true
	}
	grants := map[string][]string{
		"SRE Administrator": {"service:", "scenario:", "validation:", "agent:", "release:"},
		"SRE Operator":      {"service:read", "scenario:", "validation:", "agent:read", "release:"},
		"Developer":         {"service:read", "scenario:read", "validation:read", "release:create"},
		"Application Owner": {"service:read", "scenario:", "validation:", "release:"},
		"Auditor":           {":read"}, "Viewer": {"service:read", "scenario:read", "validation:read"},
	}
	for _, g := range grants[role] {
		if strings.HasSuffix(g, ":") && strings.HasPrefix(permission, g) || strings.HasPrefix(g, ":") && strings.HasSuffix(permission, g) || g == permission {
			return true
		}
	}
	return false
}
