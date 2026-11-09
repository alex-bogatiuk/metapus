// Command scaffold creates a typed catalog or header-only document module.
package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/*
var templates embed.FS

type scaffoldData struct {
	Name, PascalName, Key, Route, Table, Kind, Prefix string
	Document, Hierarchical                            bool
	Version                                           int
}

func main() {
	if err := run(os.Args[1:], ".", os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, root string, out io.Writer) error {
	fs := flag.NewFlagSet("scaffold", flag.ContinueOnError)
	fs.SetOutput(out)
	name := fs.String("name", "", "snake_case entity name (required)")
	kind := fs.String("type", "catalog", "catalog or document")
	table := fs.String("table", "", "SQL table (default cat_<name>s or doc_<name>s)")
	hierarchical := fs.Bool("hierarchical", false, "catalog with folders and hierarchy validation")
	version := fs.Int("migration-version", 0, "unique extension migration version (default next free version)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	valid := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	if !valid.MatchString(*name) || token.Lookup(*name).IsKeyword() {
		return fmt.Errorf("--name must be a valid snake_case Go package name")
	}
	if *kind != "catalog" && *kind != "document" {
		return fmt.Errorf("--type must be catalog or document")
	}
	if *hierarchical && *kind != "catalog" {
		return fmt.Errorf("--hierarchical is only valid for catalogs")
	}
	prefix := "cat_"
	if *kind == "document" {
		prefix = "doc_"
	}
	if *table == "" {
		*table = prefix + *name + "s"
	}
	if !valid.MatchString(*table) || !strings.HasPrefix(*table, prefix) {
		return fmt.Errorf("--table must be a valid %s SQL identifier", prefix)
	}
	used := map[int]bool{}
	migrations, err := filepath.Glob(filepath.Join(root, "extensions", "*", "migrations", "*.sql"))
	if err != nil {
		return err
	}
	next := 10001
	for _, path := range migrations {
		v, err := strconv.Atoi(strings.SplitN(filepath.Base(path), "_", 2)[0])
		if err != nil {
			continue
		}
		used[v] = true
		if v >= next {
			next = v + 1
		}
	}
	if *version == 0 {
		*version = next
	}
	if *version < 10000 || used[*version] {
		return fmt.Errorf("extension migration version %d is unavailable", *version)
	}
	pascal := ""
	for _, part := range strings.Split(*name, "_") {
		pascal += strings.ToUpper(part[:1]) + part[1:]
	}
	route := strings.ReplaceAll(*name, "_", "-")
	if *kind == "catalog" {
		route += "s"
	}
	code := strings.ToUpper(strings.ReplaceAll(*name, "_", ""))
	if len(code) > 2 {
		code = code[:2]
	}
	data := scaffoldData{Name: *name, PascalName: pascal, Key: *name, Route: route, Table: *table, Kind: *kind, Prefix: code, Document: *kind == "document", Hierarchical: *hierarchical, Version: *version}
	// Render and format everything before creating a module directory.
	files := map[string][]byte{}
	entries, err := templates.ReadDir("templates")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		source, err := templates.ReadFile("templates/" + entry.Name())
		if err != nil {
			return err
		}
		tpl, err := template.New(entry.Name()).Parse(string(source))
		if err != nil {
			return err
		}
		var buffer bytes.Buffer
		if err := tpl.Execute(&buffer, data); err != nil {
			return err
		}
		filename := strings.TrimSuffix(entry.Name(), ".tmpl")
		if filename == "migration.sql" {
			filename = filepath.Join("migrations", fmt.Sprintf("%05d_%s.sql", *version, *table))
		}
		content := buffer.Bytes()
		if strings.HasSuffix(filename, ".go") {
			content, err = format.Source(content)
			if err != nil {
				return fmt.Errorf("format %s: %w", filename, err)
			}
		}
		files[filename] = content
	}
	base := filepath.Join(root, "extensions")
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	dir := filepath.Join(base, *name)
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			return err
		}
	}
	complete = true
	_, err = fmt.Fprintf(out, "Created %s module: %s\nAdd %s.Register(reg) and its import to extensions/register.go, then run go run ./cmd/http-contracts.\n", *kind, dir, *name)
	return err
}
