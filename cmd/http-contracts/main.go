// Command http-contracts generates declarations from the explicit HTTP registry.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"metapus/internal/infrastructure/http/contracts"
)

func main() {
	check := flag.Bool("check", false, "fail if committed declarations are stale")
	output := flag.String("out", "frontend/types/generated/http-contracts.ts", "generated declaration path")
	schemas := flag.String("schemas-out", "frontend/lib/generated/http-contract-schemas.ts", "generated runtime schema path")
	routes := flag.String("routes-out", "frontend/lib/generated/http-contract-routes.ts", "generated route contract path")
	flag.Parse()
	if err := run(*check, *output, *schemas, *routes); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool, output, schemas, routePath string) error {
	data, err := contracts.Generate(contracts.Registry())
	if err != nil {
		return err
	}
	runtime, err := contracts.GenerateSchemas(contracts.Registry())
	if err != nil {
		return err
	}
	routes, err := contracts.GenerateRoutes(contracts.Registry(), contracts.Routes())
	if err != nil {
		return err
	}
	for _, artifact := range []struct {
		path string
		data []byte
	}{{output, data}, {schemas, runtime}, {routePath, routes}} {
		if err := writeArtifact(check, artifact.path, artifact.data); err != nil {
			return err
		}
	}
	fmt.Printf("Verified %d registered HTTP contracts and schemas\n", len(contracts.Registry()))
	return nil
}

func writeArtifact(check bool, output string, data []byte) error {
	if check {
		current, err := os.ReadFile(output)
		if err != nil {
			return err
		}
		if !bytes.Equal(bytes.ReplaceAll(current, []byte("\r\n"), []byte("\n")), data) {
			return fmt.Errorf("HTTP declarations are stale: run go run ./cmd/http-contracts")
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(output, data, 0644); err != nil {
		return err
	}
	return nil
}
