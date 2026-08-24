package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "backupcrypt:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || (args[0] != "encrypt" && args[0] != "decrypt") {
		return errors.New("uso: backupcrypt encrypt|decrypt --input FILE --output FILE --passphrase-file FILE")
	}
	mode := args[0]
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	inputPath := flags.String("input", "", "arquivo de entrada")
	outputPath := flags.String("output", "", "arquivo de saída")
	passphrasePath := flags.String("passphrase-file", "", "arquivo de senha")
	if err := flags.Parse(args[1:]); err != nil || *inputPath == "" || *outputPath == "" || *passphrasePath == "" {
		return errors.New("input, output e passphrase-file são obrigatórios")
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
