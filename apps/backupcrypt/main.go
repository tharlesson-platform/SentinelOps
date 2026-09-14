package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"filippo.io/age"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "backupcrypt:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithScryptWorkFactor(args, 0)
}

// runWithScryptWorkFactor keeps the CLI on age's production default while
// allowing tests to use a smaller, still authenticated scrypt envelope. A
// non-positive value deliberately means "use the library default".
func runWithScryptWorkFactor(args []string, scryptWorkFactor int) error {
	if len(args) == 0 || (args[0] != "encrypt" && args[0] != "decrypt" && args[0] != "validate") {
		return errors.New("uso: backupcrypt encrypt|decrypt --input FILE --output FILE --passphrase-file FILE | validate --input FILE")
	}
	mode := args[0]
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	inputPath := flags.String("input", "", "arquivo de entrada")
	outputPath := flags.String("output", "", "arquivo de saída")
	passphrasePath := flags.String("passphrase-file", "", "arquivo de senha")
	if err := flags.Parse(args[1:]); err != nil || *inputPath == "" {
		return errors.New("input é obrigatório")
	}
	if mode == "validate" {
		return validateArchive(*inputPath)
	}
	if *outputPath == "" || *passphrasePath == "" {
		return errors.New("output e passphrase-file são obrigatórios para encrypt/decrypt")
	}
	passphraseBytes, err := os.ReadFile(*passphrasePath)
	if err != nil {
		return fmt.Errorf("não foi possível ler passphrase-file: %w", err)
	}
	passphrase := strings.TrimSpace(string(passphraseBytes))
	for index := range passphraseBytes {
		passphraseBytes[index] = 0
	}
	if len(passphrase) < 20 {
		return errors.New("passphrase deve ter ao menos 20 caracteres")
	}
	defer func() { passphrase = "" }()

	input, err := os.Open(*inputPath)
	if err != nil {
		return fmt.Errorf("não foi possível abrir entrada: %w", err)
	}
	defer input.Close()
	output, err := os.OpenFile(*outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("não foi possível criar saída exclusiva: %w", err)
	}
	completed := false
	defer func() {
		_ = output.Close()
		if !completed {
			_ = os.Remove(*outputPath)
		}
	}()

	if mode == "encrypt" {
		recipient, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return fmt.Errorf("não foi possível preparar destinatário: %w", err)
		}
		if scryptWorkFactor > 0 {
			recipient.SetWorkFactor(scryptWorkFactor)
		}
		writer, err := age.Encrypt(output, recipient)
		if err != nil {
			return fmt.Errorf("não foi possível iniciar criptografia: %w", err)
		}
		if _, err := io.Copy(writer, input); err != nil {
			return fmt.Errorf("falha ao cifrar backup: %w", err)
		}
		if err := writer.Close(); err != nil {
			return fmt.Errorf("falha ao autenticar backup: %w", err)
		}
	} else {
		identity, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return fmt.Errorf("não foi possível preparar identidade: %w", err)
		}
		reader, err := age.Decrypt(input, identity)
		if err != nil {
			return errors.New("senha incorreta ou backup não autenticado")
		}
		if _, err := io.Copy(output, reader); err != nil {
			return errors.New("backup adulterado, truncado ou não autenticado")
		}
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("falha ao sincronizar saída: %w", err)
	}
	completed = true
	return output.Close()
}

var safeProjectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var gitRevision = regexp.MustCompile(`^(unknown|[0-9a-f]{40})$`)

type backupMetadata struct {
	SchemaVersion int      `json:"schemaVersion"`
	CreatedAt     string   `json:"createdAt"`
	SourceProject string   `json:"sourceProject"`
	GitRevision   string   `json:"gitRevision"`
	Databases     []string `json:"databases"`
	Bucket        string   `json:"bucket"`
}

func validateArchive(inputPath string) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("não foi possível abrir backup: %w", err)
	}
	defer input.Close()
	gzipReader, err := gzip.NewReader(input)
	if err != nil {
		return errors.New("backup não contém um tar.gz válido")
	}
	defer gzipReader.Close()

	required := map[string]bool{
		"databases/sentinel.dump":            false,
		"databases/temporal.dump":            false,
		"databases/temporal_visibility.dump": false,
		"databases/globals.sql":              false,
		"metadata.json":                      false,
		"SHA256SUMS":                         false,
	}
	reader := tar.NewReader(gzipReader)
	var metadata []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("backup tar inválido ou truncado")
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name == "" || path.IsAbs(header.Name) || path.Clean(name) != name || strings.HasPrefix(name, "../") {
			return errors.New("backup contém caminho inseguro")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return errors.New("backup contém link ou tipo de arquivo não permitido")
		}
		_, isRequired := required[name]
		isAllowedDirectory := name == "databases" || name == "minio" || name == "minio/sentinel-artifacts"
		isMinioObject := strings.HasPrefix(name, "minio/sentinel-artifacts/")
		if !isRequired && !isAllowedDirectory && !isMinioObject {
			return errors.New("backup contém arquivo fora do contrato")
		}
		if isRequired {
			if required[name] || header.Typeflag == tar.TypeDir || header.Size <= 0 {
				return errors.New("backup não atende ao contrato de arquivos obrigatórios")
			}
			required[name] = true
		}
		if name == "metadata.json" {
			if header.Size > 64*1024 {
				return errors.New("metadata do backup excede o limite permitido")
			}
			metadata, err = io.ReadAll(reader)
		} else {
			_, err = io.Copy(io.Discard, reader)
		}
		if err != nil {
			return errors.New("backup tar inválido ou truncado")
		}
	}
	for name, present := range required {
		if !present {
			return fmt.Errorf("backup não atende ao contrato: arquivo obrigatório ausente: %s", name)
		}
	}
	if err := validateMetadata(metadata); err != nil {
		return err
	}
	return nil
}

func validateMetadata(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var metadata backupMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return errors.New("metadata do backup não atende ao contrato SentinelOps v1")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("metadata do backup não atende ao contrato SentinelOps v1")
	}
	if decoder.More() || metadata.SchemaVersion != 1 || !safeProjectName.MatchString(metadata.SourceProject) || !gitRevision.MatchString(metadata.GitRevision) || metadata.Bucket != "sentinel-artifacts" || len(metadata.Databases) != 3 || metadata.Databases[0] != "sentinel" || metadata.Databases[1] != "temporal" || metadata.Databases[2] != "temporal_visibility" {
		return errors.New("metadata do backup não atende ao contrato SentinelOps v1")
	}
	createdAt, err := time.Parse(time.RFC3339, metadata.CreatedAt)
	if err != nil || createdAt.Location() != time.UTC || createdAt.Format(time.RFC3339) != metadata.CreatedAt {
		return errors.New("metadata do backup não atende ao contrato SentinelOps v1")
	}
	return nil
}
