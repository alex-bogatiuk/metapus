// Package dbassets embeds PostgreSQL migrations for binaries started outside the repository root.
package dbassets

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Files contains the platform's tenant migrations (migrations/), each
// configuration's tenant migrations (configuration/<name>/) and the
// control-plane meta migrations (meta/).
//
//go:embed migrations/*.sql meta/*.sql configuration/*/*.sql
var Files embed.FS

var migrationName = regexp.MustCompile(`^(\d{5})_[a-z0-9_]+\.sql$`)

// TenantMigrations returns one flat directory with the platform's and every
// configuration's tenant migrations. They share one version sequence and one
// goose history: a version used twice, in any directory, is an error.
func TenantMigrations() (fs.FS, error) { return tenantMigrations(Files) }

func tenantMigrations(source fs.FS) (fs.FS, error) {
	dirs := []string{"migrations"}
	configurations, err := fs.Glob(source, "configuration/*")
	if err != nil {
		return nil, err
	}
	sort.Strings(configurations)
	dirs = append(dirs, configurations...)
	files := map[string][]byte{}
	versions := map[string]string{}
	for _, dir := range dirs {
		entries, err := fs.ReadDir(source, dir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || path.Ext(entry.Name()) != ".sql" {
				continue
			}
			match := migrationName.FindStringSubmatch(entry.Name())
			if match == nil {
				return nil, fmt.Errorf("%s/%s: migration file name must be NNNNN_name.sql", dir, entry.Name())
			}
			if other, taken := versions[match[1]]; taken {
				return nil, fmt.Errorf("migration version %s is used by %s and %s/%s", match[1], other, dir, entry.Name())
			}
			raw, err := fs.ReadFile(source, path.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			versions[match[1]] = path.Join(dir, entry.Name())
			files[entry.Name()] = raw
		}
	}
	return flatFS(files), nil
}

// CheckMigrationFormat reports a tenant migration that does not run each
// direction as one goose statement: "-- +goose Up" and "-- +goose Down" are
// each followed directly by one StatementBegin … StatementEnd block. Tests
// apply it to the shipped migrations and to what cmd/scaffold generates.
func CheckMigrationFormat(sql string) error {
	const begin, end = "-- +goose StatementBegin", "-- +goose StatementEnd"
	remaining := []string{"-- +goose Up", begin, end, "-- +goose Down", begin, end}
	lineNumber := 0
	for line := range strings.Lines(sql) {
		lineNumber++
		line = strings.TrimRight(line, " \t\r\n")
		if len(remaining) > 0 && line == remaining[0] {
			remaining = remaining[1:]
			continue
		}
		if len(remaining) > 0 && remaining[0] == begin {
			return fmt.Errorf("line %d: expected %s immediately after direction", lineNumber, begin)
		}
		comment := strings.TrimSpace(line)
		if strings.HasPrefix(comment, "--") && strings.Contains(comment, "+goose") {
			switch strings.ToUpper(line) {
			case "-- +GOOSE NO TRANSACTION", "-- +GOOSE ENVSUB ON", "-- +GOOSE ENVSUB OFF":
				continue
			}
			return fmt.Errorf("line %d: unexpected goose directive %q", lineNumber, line)
		}
		if line == "" || strings.HasPrefix(comment, "--") {
			continue
		}
		if len(remaining) == 0 || remaining[0] != end {
			return fmt.Errorf("line %d: SQL outside StatementBegin … StatementEnd", lineNumber)
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("expected %s before end of migration", remaining[0])
	}
	return nil
}

// flatFS is a read-only in-memory directory of files.
type flatFS map[string][]byte

func (f flatFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &flatDir{fs: f}, nil
	}
	raw, ok := f[name]
	if !ok || !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &flatFile{info: flatInfo{name: name, size: int64(len(raw))}, data: raw}, nil
}

func (f flatFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	names := make([]string, 0, len(f))
	for n := range f {
		names = append(names, n)
	}
	sort.Strings(names)
	entries := make([]fs.DirEntry, len(names))
	for i, n := range names {
		entries[i] = fs.FileInfoToDirEntry(flatInfo{name: n, size: int64(len(f[n]))})
	}
	return entries, nil
}

type flatInfo struct {
	name string
	size int64
	dir  bool
}

func (i flatInfo) Name() string { return i.name }
func (i flatInfo) Size() int64  { return i.size }
func (i flatInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i flatInfo) ModTime() time.Time { return time.Time{} }
func (i flatInfo) IsDir() bool        { return i.dir }
func (i flatInfo) Sys() any           { return nil }

type flatFile struct {
	info   flatInfo
	data   []byte
	offset int
}

func (f *flatFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *flatFile) Close() error               { return nil }
func (f *flatFile) Read(p []byte) (int, error) {
	if f.offset >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.offset:])
	f.offset += n
	return n, nil
}

type flatDir struct {
	fs     flatFS
	listed bool
}

func (d *flatDir) Stat() (fs.FileInfo, error) { return flatInfo{name: ".", dir: true}, nil }
func (d *flatDir) Close() error               { return nil }
func (d *flatDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}
func (d *flatDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.listed {
		if n > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	d.listed = true
	return d.fs.ReadDir(".")
}
