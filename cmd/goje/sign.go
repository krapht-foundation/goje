package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// keygen writes a new Ed25519 key pair as PREFIX.key, a PKCS #8 PEM file that only
// the owner can read, and PREFIX.pub, a PKIX PEM file. It never replaces a file.
func keygen(args []string) error {
	set := flag.NewFlagSet("goje keygen", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	prefix := set.String("o", "goje-signing", "output path prefix")
	if err := set.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	if set.NArg() != 0 || *prefix == "" {
		return fmt.Errorf("%w: keygen takes only a non-empty -o PREFIX", errUsage)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return fmt.Errorf("encode private key: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return fmt.Errorf("encode public key: %w", err)
	}
	keyFile := *prefix + ".key"
	if createErr := create(
		keyFile,
		0o600,
		&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER},
	); createErr != nil {
		return createErr
	}
	err = create(*prefix+".pub", 0o644, &pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	if err != nil {
		// A private key without its public half is useless and must not linger.
		return errors.Join(err, os.Remove(keyFile))
	}
	return nil
}

func create(name string, mode os.FileMode, block *pem.Block) (err error) {
	// #nosec G304 -- The developer chose the path.
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := pem.Encode(file, block); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// readPrivateKey reads the PEM file that keygen writes.
func readPrivateKey(name string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(name) // #nosec G304 -- The developer chose the key file.
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("signing key %s: want a PEM PRIVATE KEY block", name)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("signing key %s: %w", name, err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("signing key %s is %T, not Ed25519", name, parsed)
	}
	return key, nil
}
