package main

import (
	"archive/tar"
	"compress/gzip"
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
	if err := runWithScryptWorkFactor([]string{"encrypt", "--input", input, "--output", encrypted, "--passphrase-file", passphrase}, 10); err != nil {
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

func TestValidateArchiveRejectsLinksAndUnexpectedPaths(t *testing.T) {
	directory := t.TempDir()
	valid := filepath.Join(directory, "valid.tar.gz")
	writeArchive(t, valid, false, "")
	if err := run([]string{"validate", "--input", valid}); err != nil {
		t.Fatalf("valid archive: %v", err)
	}
	for _, test := range []struct {
		name  string
		link  bool
		extra string
	}{
		{name: "link", link: true},
		{name: "unexpected path", extra: "outside-contract.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(directory, test.name+".tar.gz")
			writeArchive(t, archive, test.link, test.extra)
			if err := run([]string{"validate", "--input", archive}); err == nil {
				t.Fatal("archive outside contract was accepted")
			}
		})
	}
}

func writeArchive(t *testing.T, filename string, link bool, extra string) {
	t.Helper()
	output, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(output)
	writer := tar.NewWriter(gzipWriter)
	files := map[string]string{
		"databases/sentinel.dump":            "sentinel",
		"databases/temporal.dump":            "temporal",
		"databases/temporal_visibility.dump": "visibility",
		"databases/globals.sql":              "globals",
		"SHA256SUMS":                         "checksum",
		"metadata.json":                      `{"schemaVersion":1,"createdAt":"2026-09-09T12:00:00Z","sourceProject":"sentinelops","gitRevision":"unknown","databases":["sentinel","temporal","temporal_visibility"],"bucket":"sentinel-artifacts"}`,
	}
	for name, contents := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if link {
		if err := writer.WriteHeader(&tar.Header{Name: "minio/sentinel-artifacts/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp/outside"}); err != nil {
			t.Fatal(err)
		}
	}
	if extra != "" {
		contents := "unexpected"
		if err := writer.WriteHeader(&tar.Header{Name: extra, Mode: 0o600, Size: int64(len(contents))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
