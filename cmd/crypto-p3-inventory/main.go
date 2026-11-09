// crypto-p3-inventory lists mutation entry points for the finite P3 review.
// It reads source code only; it never connects to a tenant database.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var sqlMutation = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO\b|UPDATE\b|DELETE\s+FROM\b|ON\s+CONFLICT\b)`)
var sqlTarget = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+((?:reg|doc|cat|sys)_[a-z0-9_]+)`)
var sqlFunction = regexp.MustCompile(`\bfn_(?:crypto|external_outflow|reconciliation)[a-z0-9_]*`)
var writeCall = regexp.MustCompile(`(?i)^(?:Create|Update|Upsert|Insert|Delete|Save|Write|Put|Post|Unpost|Repost|Record|Register|Reserve|Append|Acquire|Resolve|Set|Mark|Cancel|Retry|Claim|Release|Complete|Finalize|Reject|Approve|Classify|Enqueue|Publish|Capture|Apply|Lock|Exec|Query|CopyFrom|SendBatch|RunInTransaction)`)

func participating(path string) bool {
	if !strings.HasPrefix(path, "internal/") && !strings.HasPrefix(path, "cmd/") {
		return false
	}
	if strings.HasPrefix(path, "internal/infrastructure/storage/postgres/") {
		return true
	}
	for _, part := range []string{"crypto", "outbound", "treasury", "/posting/", "/wallet/", "/token/", "/blockchain/", "/storage/base", "/domain/base", "/domain/document", "/domain/header", "/domain/catalog", "/handlers/document", "/handlers/catalog"} {
		if strings.Contains(path, part) {
			return true
		}
	}
	return false
}

func joined(values map[string]bool) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.Join(keys, ";")
}

func run(root string) error {
	out := csv.NewWriter(os.Stdout)
	if err := out.Write([]string{"path", "function", "line", "body_sha256", "sql_write", "on_conflict", "sql_targets", "sql_functions", "mutation_or_lock_calls"}); err != nil {
		return err
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" || strings.HasPrefix(entry.Name(), ".tmp") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || !participating(rel) || strings.HasPrefix(rel, "cmd/crypto-p3-inventory/") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, rel, source, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			targets, functions, calls := map[string]bool{}, map[string]bool{}, map[string]bool{}
			write, conflict := false, false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					value, err := strconv.Unquote(literal.Value)
					if err == nil {
						write = write || sqlMutation.MatchString(value)
						conflict = conflict || strings.Contains(strings.ToUpper(value), "ON CONFLICT")
						for _, match := range sqlTarget.FindAllStringSubmatch(value, -1) {
							targets[strings.ToLower(match[1])] = true
						}
						for _, match := range sqlFunction.FindAllString(value, -1) {
							functions[match] = true
						}
					}
				}
				if call, ok := node.(*ast.CallExpr); ok {
					name := ""
					switch expr := call.Fun.(type) {
					case *ast.SelectorExpr:
						name = expr.Sel.Name
					case *ast.Ident:
						name = expr.Name
					}
					if writeCall.MatchString(name) {
						calls[name] = true
					}
				}
				return true
			})
			if !write && len(functions) == 0 && len(calls) == 0 {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil {
				var receiver bytes.Buffer
				if err := format.Node(&receiver, positions, fn.Recv.List[0].Type); err != nil {
					return err
				}
				name = receiver.String() + "." + name
			}
			start, end := positions.Position(fn.Pos()).Offset, positions.Position(fn.End()).Offset
			hash := fmt.Sprintf("%x", sha256.Sum256(source[start:end]))
			if err := out.Write([]string{rel, name, strconv.Itoa(positions.Position(fn.Pos()).Line), hash, strconv.FormatBool(write), strconv.FormatBool(conflict), joined(targets), joined(functions), joined(calls)}); err != nil {
				return err
			}
		}
		return nil
	})
	out.Flush()
	if err != nil {
		return err
	}
	return out.Error()
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if err := run(*root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
