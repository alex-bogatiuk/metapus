// Command http-contracts generates the frontend wire declarations, runtime
// schemas and route tables from the explicit HTTP registry: the platform's
// files and each configuration's own files on top of them.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"metapus/extensions"
	"metapus/internal/configuration"
	"metapus/internal/content"
)

func main() {
	check := flag.Bool("check", false, "fail if committed declarations are stale")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool) error {
	artifacts, count, err := configuration.HTTPArtifacts(content.Crypto(content.Services{}), extensions.Register)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := writeArtifact(check, artifact.Path, artifact.Data); err != nil {
			return err
		}
	}
	fmt.Printf("Verified %d registered HTTP contracts and schemas\n", count)
	return nil
}

func writeArtifact(check bool, output string, data []byte) error {
	if check {
		current, err := os.ReadFile(output)
		if err != nil {
			return err
		}
		if !bytes.Equal(bytes.ReplaceAll(current, []byte("\r\n"), []byte("\n")), data) {
			return fmt.Errorf("HTTP declarations are stale: run go run ./cmd/http-contracts (%s)", output)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	return os.WriteFile(output, data, 0644)
}
