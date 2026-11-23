// Command http-contracts generates the frontend wire declarations, runtime
// schemas and route tables from the explicit HTTP registry: the platform's
// files and, for every configuration of the repository, its own files on top
// of them. The platform's files describe the platform alone, so every
// configuration must produce the same ones.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/internal/configuration"
	autoparts "github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/content"
	crypto "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
	"github.com/alex-bogatiuk/metapus/internal/configuration/none"
)

// configurations are the repository's configurations whose frontend files
// the command generates.
func configurations() []configuration.Configuration {
	return []configuration.Configuration{none.New(), crypto.Crypto(crypto.Services{}), autoparts.Autoparts()}
}

func main() {
	check := flag.Bool("check", false, "fail if committed declarations are stale")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool) error {
	files, err := artifacts(configurations())
	if err != nil {
		return err
	}
	for _, artifact := range files.list {
		if err := writeArtifact(check, artifact.Path, artifact.Data); err != nil {
			return err
		}
	}
	for _, line := range files.counts {
		fmt.Println(line)
	}
	return nil
}

type generated struct {
	list   []configuration.HTTPArtifact
	counts []string
}

// artifacts collects every configuration's files, each path once; a path two
// configurations generate differently is an error.
func artifacts(cfgs []configuration.Configuration) (generated, error) {
	var result generated
	owner := map[string]string{}
	data := map[string][]byte{}
	for _, cfg := range cfgs {
		files, count, err := configuration.HTTPArtifacts(cfg, extensions.Register)
		if err != nil {
			return result, fmt.Errorf("%s: %w", cfg.Name(), err)
		}
		for _, file := range files {
			if previous, ok := data[file.Path]; ok {
				if !bytes.Equal(previous, file.Data) {
					return result, fmt.Errorf("%s differs between the %s and %s configurations; the platform's files must not depend on a configuration", file.Path, owner[file.Path], cfg.Name())
				}
				continue
			}
			owner[file.Path], data[file.Path] = cfg.Name(), file.Data
			result.list = append(result.list, file)
		}
		result.counts = append(result.counts, fmt.Sprintf("Verified %d registered HTTP contracts and schemas of %s", count, cfg.Name()))
	}
	return result, nil
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
