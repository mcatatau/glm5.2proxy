package zcodeenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"glm5.2proxy/internal/accounts"
)

func TestCipherRoundTrip(t *testing.T) {
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	cipher := NewCipher(t.TempDir())
	encrypted, err := cipher.Encrypt("valor secreto")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "valor secreto" {
		t.Fatal("expected encrypted value")
	}
	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "valor secreto" {
		t.Fatalf("unexpected decrypted value: %q", decrypted)
	}
}

func TestWriteCredentialsPreservesOtherKeys(t *testing.T) {
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte(`{"other":"value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	account := accounts.Account{
		User:            accounts.User{UserID: "u1", Email: "u1@example.com", Name: "User 1"},
		ZCodeJWTToken:   "jwt-token",
		ZAIAcccessToken: "access-token",
	}
	backup, err := writeCredentials(path, NewCipher(dir), account)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected backup path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["other"] != "value" {
		t.Fatal("other credential key was not preserved")
	}
	plain, err := NewCipher(dir).Decrypt(saved[credentialJWTToken])
	if err != nil {
		t.Fatal(err)
	}
	if plain != "jwt-token" {
		t.Fatalf("unexpected jwt: %q", plain)
	}
}

func TestWriteCredentialsAllowsJWTOnlyAccount(t *testing.T) {
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	account := accounts.Account{
		User:          accounts.User{UserID: "u1", Email: "u1@example.com", Name: "User 1"},
		ZCodeJWTToken: "jwt-token",
	}
	if _, err := writeCredentials(path, NewCipher(dir), account); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]string
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved[credentialAccessToken] != "" {
		t.Fatal("did not expect access token credential for jwt-only account")
	}
	if saved[credentialJWTToken] == "" {
		t.Fatal("expected jwt credential")
	}
}
