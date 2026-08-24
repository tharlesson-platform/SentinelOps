package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptAndRejectTampering(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "input")
	passphrase := filepath.Join(directory, "passphrase")
	encrypted := filepath.Join(directory, "backup.age")
	decrypted := filepath.Join(directory, "decrypted")
	if err := os.WriteFile(input, []byte("sentinelops-backup-proof"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passphrase, []byte("correct horse battery staple proof"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"encrypt", "--input", input, "--output", encrypted, "--passphrase-file", passphrase}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := run([]string{"decrypt", "--input", encrypted, "--output", decrypted, "--passphrase-file", passphrase}); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	got, err := os.ReadFile(decrypted)
	if err != nil || string(got) != "sentinelops-backup-proof" {
		t.Fatalf("round trip inválido: %q, %v", got, err)
	}

	ciphertext, err := os.ReadFile(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 0xff
	tampered := filepath.Join(directory, "tampered.age")
	if err := os.WriteFile(tampered, ciphertext, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"decrypt", "--input", tampered, "--output", filepath.Join(directory, "must-not-exist"), "--passphrase-file", passphrase}); err == nil {
		t.Fatal("ciphertext adulterado foi aceito")
	}
}
