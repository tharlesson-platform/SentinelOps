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
