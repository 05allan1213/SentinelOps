package auth

import (
	"testing"
	"time"
)

func TestConfigRejectsEmptyJWTSecret(t *testing.T) {
	if err := Init(nil); err == nil {
		t.Fatal("Init() accepted an empty JWT secret")
	}
	if err := Init([]byte("test-jwt-secret-with-sufficient-entropy")); err != nil {
		t.Fatal(err)
	}
	token, err := Generate("user-id", "tester", "viewer", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "user-id" || claims.Role != "viewer" {
		t.Fatalf("claims = %#v", claims)
	}
}
