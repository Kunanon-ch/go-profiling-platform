// Command set-module-path changes this repository's Go module path and every
// Go import that refers to the old path. It is intentionally implemented with
// only the standard library so it can repair a checkout after go.mod was
// changed manually and the smoke command no longer compiles.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const placeholderModulePath = "profiler-probe"

type fileChange struct {
	path   string
	before []byte
	after  []byte
	mode   fs.FileMode
}

type migrationResult struct {
	oldModule   string
	newModule   string
	changedFile []string
}

func main() {
	root := flag.String("root", ".", "repository root containing go.mod")
	verify := flag.Bool("verify", true, "run module and smoke-test checks, rolling back on failure")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: go run ./cmd/set-module-path [options] <new-module-path>\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	result, err := migrate(*root, flag.Arg(0), *verify)
	if err != nil {
		fmt.Fprintf(os.Stderr, "module-path migration failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Go module path: %s -> %s\n", result.oldModule, result.newModule)
	if len(result.changedFile) == 0 {
		fmt.Println("No files needed changes; verification passed.")
		return
	}
	fmt.Println("Updated files:")
	for _, path := range result.changedFile {
		fmt.Printf("- %s\n", path)
	}
	if *verify {
		fmt.Println("Module checks and smoke-tag tests passed.")
	} else {
		fmt.Println("Verification was explicitly skipped; run make verify before committing.")
	}
}

func migrate(root, target string, verify bool) (migrationResult, error) {
	result := migrationResult{newModule: target}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return result, fmt.Errorf("resolve repository root: %w", err)
	}
	goModPath := filepath.Join(absRoot, "go.mod")
	goMod, goModMode, err := readFileWithMode(goModPath)
	if err != nil {
		return result, fmt.Errorf("read go.mod: %w", err)
	}

	oldModule, err := currentModulePath(absRoot)
	if err != nil {
		return result, err
	}
	result.oldModule = oldModule

	updatedGoMod, err := renderGoMod(absRoot, goMod, goModMode, target)
	if err != nil {
		return result, err
	}

	fromPaths := []string{oldModule}
	if oldModule != placeholderModulePath {
		// This second source makes the command self-repairing when somebody has
		// already edited go.mod but left the bootstrap import unchanged.
		fromPaths = append(fromPaths, placeholderModulePath)
	}
	changes, err := internalImportChanges(absRoot, fromPaths, target)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(goMod, updatedGoMod) {
		changes = append(changes, fileChange{
			path:   goModPath,
			before: goMod,
			after:  updatedGoMod,
			mode:   goModMode,
		})
	}

	if err := applyChanges(changes); err != nil {
		return result, err
	}

	if verify {
		if err := verifyMigration(absRoot, target); err != nil {
			rollbackErr := rollbackChanges(changes)
			if rollbackErr != nil {
				return result, errors.Join(
					fmt.Errorf("verification failed and migration was rolled back: %w", err),
					fmt.Errorf("rollback was incomplete: %w", rollbackErr),
				)
			}
			return result, fmt.Errorf("verification failed; all migration edits were rolled back: %w", err)
		}
	}

	for _, change := range changes {
		relative, relErr := filepath.Rel(absRoot, change.path)
		if relErr != nil {
			relative = change.path
		}
		result.changedFile = append(result.changedFile, filepath.ToSlash(relative))
	}
	sort.Strings(result.changedFile)
	return result, nil
}

func currentModulePath(root string) (string, error) {
	command := isolatedGoCommand(root, "mod", "edit", "-json")
	output, err := command.Output()
	if err != nil {
		return "", commandError("read current module path", command, err)
	}
	var document struct {
		Module *struct {
			Path string
		}
	}
	if err := json.Unmarshal(output, &document); err != nil {
		return "", fmt.Errorf("decode go mod edit output: %w", err)
	}
	if document.Module == nil || document.Module.Path == "" {
		return "", errors.New("go.mod has no module directive")
	}
	return document.Module.Path, nil
}

func renderGoMod(root string, original []byte, mode fs.FileMode, target string) ([]byte, error) {
	temporary, err := os.CreateTemp(root, ".set-module-path-*.mod")
	if err != nil {
		return nil, fmt.Errorf("create temporary module file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if _, err := temporary.Write(original); err != nil {
		temporary.Close()
		return nil, fmt.Errorf("write temporary module file: %w", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return nil, fmt.Errorf("set temporary module-file mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("close temporary module file: %w", err)
	}

	command := isolatedGoCommand(root, "mod", "edit", "-modfile="+temporaryPath, "-module="+target)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("validate module path %q: %w", target, outputError(err, output))
	}
	updated, err := os.ReadFile(temporaryPath)
	if err != nil {
		return nil, fmt.Errorf("read updated temporary module file: %w", err)
	}
	return updated, nil
}

func internalImportChanges(root string, fromPaths []string, target string) ([]fileChange, error) {
	excludedDirectories := map[string]bool{
		".git":   true,
		".helm":  true,
		"dist":   true,
		"vendor": true,
	}
	var changes []fileChange
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && excludedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		content, mode, err := readFileWithMode(path)
		if err != nil {
			return err
		}
		updated, err := rewriteImports(path, content, fromPaths, target)
		if err != nil {
			return err
		}
		if !bytes.Equal(content, updated) {
			changes = append(changes, fileChange{
				path:   path,
				before: content,
				after:  updated,
				mode:   mode,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan Go imports: %w", err)
	}
	return changes, nil
}

func rewriteImports(filename string, content []byte, fromPaths []string, target string) ([]byte, error) {
	files := token.NewFileSet()
	syntax, err := parser.ParseFile(files, filename, content, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}

	type replacement struct {
		start int
		end   int
		value []byte
	}
	var replacements []replacement
	for _, imported := range syntax.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("decode import in %s: %w", filename, err)
		}
		updated := path
		for _, from := range fromPaths {
			if path == from {
				updated = target
				break
			}
			if strings.HasPrefix(path, from+"/") {
				updated = target + strings.TrimPrefix(path, from)
				break
			}
		}
		if updated == path {
			continue
		}
		replacements = append(replacements, replacement{
			start: files.Position(imported.Path.Pos()).Offset,
			end:   files.Position(imported.Path.End()).Offset,
			value: []byte(strconv.Quote(updated)),
		})
	}

	if len(replacements) == 0 {
		return append([]byte(nil), content...), nil
	}
	var updated bytes.Buffer
	updated.Grow(len(content))
	cursor := 0
	for _, replacement := range replacements {
		updated.Write(content[cursor:replacement.start])
		updated.Write(replacement.value)
		cursor = replacement.end
	}
	updated.Write(content[cursor:])
	return updated.Bytes(), nil
}

func applyChanges(changes []fileChange) error {
	return applyChangesWithWriter(changes, os.WriteFile)
}

func applyChangesWithWriter(changes []fileChange, writeFile func(string, []byte, fs.FileMode) error) error {
	for _, change := range changes {
		current, err := os.ReadFile(change.path)
		if err != nil {
			return fmt.Errorf("re-read %s before update: %w", change.path, err)
		}
		if !bytes.Equal(current, change.before) {
			return fmt.Errorf("%s changed while the migration was being prepared", change.path)
		}
	}

	for index, change := range changes {
		if err := writeFile(change.path, change.after, change.mode); err != nil {
			// A failed write is allowed to have truncated or partially replaced
			// the current file. Restore it as well as every prior file.
			rollbackErr := rollbackChanges(changes[:index+1])
			if rollbackErr != nil {
				return errors.Join(
					fmt.Errorf("write %s: %w", change.path, err),
					fmt.Errorf("rollback was incomplete: %w", rollbackErr),
				)
			}
			return fmt.Errorf("write %s; earlier edits were rolled back: %w", change.path, err)
		}
	}
	return nil
}

func rollbackChanges(changes []fileChange) error {
	var rollbackErrors []error
	for index := len(changes) - 1; index >= 0; index-- {
		change := changes[index]
		if err := os.WriteFile(change.path, change.before, change.mode); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", change.path, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func verifyMigration(root, target string) error {
	checks := []struct {
		name string
		args []string
	}{
		{name: "module path", args: []string{"list", "-m", "-f={{.Path}}"}},
		{name: "module tidiness", args: []string{"mod", "tidy", "-diff"}},
		{name: "module checksums", args: []string{"mod", "verify"}},
		{name: "Go tests", args: []string{"test", "./..."}},
		{name: "smoke-tag tests", args: []string{"test", "-tags", "smoke", "./cmd/pyroscope-smoke"}},
	}
	for _, check := range checks {
		command := isolatedGoCommand(root, check.args...)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w", check.name, outputError(err, output))
		}
		if check.name == "module path" && strings.TrimSpace(string(output)) != target {
			return fmt.Errorf("module path: got %q, want %q", strings.TrimSpace(string(output)), target)
		}
	}
	return nil
}

// isolatedGoCommand keeps repository-local operations independent from an
// enclosing corporate go.work. Without this, go list -m can return multiple
// workspace modules and tests can accidentally use sibling module sources.
func isolatedGoCommand(root string, args ...string) *exec.Cmd {
	command := exec.Command("go", args...)
	command.Dir = root
	command.Env = environmentWith("GOWORK", "off")
	return command
}

func environmentWith(key, value string) []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment)+1)
	for _, assignment := range environment {
		name, _, found := strings.Cut(assignment, "=")
		if found && strings.EqualFold(name, key) {
			continue
		}
		filtered = append(filtered, assignment)
	}
	return append(filtered, key+"="+value)
}

func readFileWithMode(path string) ([]byte, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("refusing to edit symbolic link %s", path)
	}
	content, err := os.ReadFile(path)
	return content, info.Mode(), err
}

func commandError(operation string, command *exec.Cmd, err error) error {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return fmt.Errorf("%s: %w", operation, outputError(err, exitError.Stderr))
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func outputError(err error, output []byte) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}
