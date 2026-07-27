package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const corporateModulePath = "git.company.example/platform/profilerprobe"

func TestMigrateUpdatesModuleAndInternalImports(t *testing.T) {
	root := newFixture(t, placeholderModulePath, placeholderModulePath, true)

	result, err := migrate(root, corporateModulePath, true)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if result.oldModule != placeholderModulePath || result.newModule != corporateModulePath {
		t.Fatalf("unexpected result: %#v", result)
	}
	assertContains(t, filepath.Join(root, "go.mod"), "module "+corporateModulePath)
	assertContains(t, filepath.Join(root, "cmd", "pyroscope-smoke", "main.go"), `probe "`+corporateModulePath+`"`)
	assertContains(t, filepath.Join(root, "internal", "consumer", "consumer.go"), `"`+corporateModulePath+`/internal/value"`)
	assertNotContains(t, filepath.Join(root, "cmd", "pyroscope-smoke", "main.go"), `"`+placeholderModulePath+`"`)
}

func TestMigrateRepairsPlaceholderAfterManualGoModEdit(t *testing.T) {
	root := newFixture(t, corporateModulePath, placeholderModulePath, true)

	if _, err := migrate(root, corporateModulePath, true); err != nil {
		t.Fatalf("repair migration: %v", err)
	}
	assertContains(t, filepath.Join(root, "cmd", "pyroscope-smoke", "main.go"), `probe "`+corporateModulePath+`"`)
}

func TestMigrateIgnoresActiveParentWorkspace(t *testing.T) {
	root := newFixture(t, placeholderModulePath, placeholderModulePath, true)
	workspace := filepath.Dir(root)
	other := filepath.Join(workspace, "other")
	writeTestFile(t, filepath.Join(other, "go.mod"), "module example.test/other\n\ngo 1.25.0\n")
	writeTestFile(t, filepath.Join(other, "other.go"), "package other\n")
	goWork := filepath.Join(workspace, "go.work")
	writeTestFile(t, goWork, "go 1.25.0\n\nuse (\n\t./repo\n\t./other\n)\n")
	t.Setenv("GOWORK", goWork)

	if _, err := migrate(root, corporateModulePath, true); err != nil {
		t.Fatalf("migration inherited parent workspace: %v", err)
	}
	assertContains(t, filepath.Join(root, "go.mod"), "module "+corporateModulePath)
	assertContains(t, filepath.Join(other, "go.mod"), "module example.test/other")
}

func TestMigrateRejectsInvalidTargetWithoutEditing(t *testing.T) {
	root := newFixture(t, placeholderModulePath, placeholderModulePath, true)
	goModPath := filepath.Join(root, "go.mod")
	smokePath := filepath.Join(root, "cmd", "pyroscope-smoke", "main.go")
	goModBefore := readTestFile(t, goModPath)
	smokeBefore := readTestFile(t, smokePath)

	if _, err := migrate(root, "bad module path", false); err == nil {
		t.Fatal("expected invalid module path to fail")
	}
	if got := readTestFile(t, goModPath); got != goModBefore {
		t.Fatal("go.mod changed after rejected migration")
	}
	if got := readTestFile(t, smokePath); got != smokeBefore {
		t.Fatal("smoke import changed after rejected migration")
	}
}

func TestMigrateRollsBackWhenVerificationFails(t *testing.T) {
	root := newFixture(t, placeholderModulePath, placeholderModulePath, false)
	goModPath := filepath.Join(root, "go.mod")
	smokePath := filepath.Join(root, "cmd", "pyroscope-smoke", "main.go")
	goModBefore := readTestFile(t, goModPath)
	smokeBefore := readTestFile(t, smokePath)

	if _, err := migrate(root, corporateModulePath, true); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected verified migration to roll back, got %v", err)
	}
	if got := readTestFile(t, goModPath); got != goModBefore {
		t.Fatal("go.mod was not rolled back")
	}
	if got := readTestFile(t, smokePath); got != smokeBefore {
		t.Fatal("smoke import was not rolled back")
	}
}

func TestApplyChangesRestoresCurrentFileAfterPartialWrite(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.go")
	secondPath := filepath.Join(root, "go.mod")
	writeTestFile(t, firstPath, "first-before")
	writeTestFile(t, secondPath, "second-before")
	changes := []fileChange{
		{path: firstPath, before: []byte("first-before"), after: []byte("first-after"), mode: 0o644},
		{path: secondPath, before: []byte("second-before"), after: []byte("second-after"), mode: 0o644},
	}
	writes := 0
	partialWriter := func(path string, content []byte, mode fs.FileMode) error {
		writes++
		if writes == 2 {
			if err := os.WriteFile(path, []byte("partial"), mode); err != nil {
				return err
			}
			return errors.New("injected write failure")
		}
		return os.WriteFile(path, content, mode)
	}

	if err := applyChangesWithWriter(changes, partialWriter); err == nil {
		t.Fatal("expected injected write failure")
	}
	if got := readTestFile(t, firstPath); got != "first-before" {
		t.Fatalf("prior file was not restored: %q", got)
	}
	if got := readTestFile(t, secondPath); got != "second-before" {
		t.Fatalf("partially written current file was not restored: %q", got)
	}
}

func TestInternalImportChangesRejectsGoSymlink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.go")
	outsideContent := "package outside\n\nimport _ \"profiler-probe\"\n"
	writeTestFile(t, outside, outsideContent)
	root := t.TempDir()
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	if _, err := internalImportChanges(root, []string{placeholderModulePath}, corporateModulePath); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symbolic-link rejection, got %v", err)
	}
	if got := readTestFile(t, outside); got != outsideContent {
		t.Fatalf("file outside repository was modified through symlink:\n%s", got)
	}
}

func TestRewriteImportsLeavesCommentsAndExternalImportsUntouched(t *testing.T) {
	source := []byte("package example\n\nimport (\n\t\"fmt\"\n\told \"profiler-probe/subpackage\"\n)\n\n// profiler-probe is documentation.\n")
	updated, err := rewriteImports("example.go", source, []string{placeholderModulePath}, corporateModulePath)
	if err != nil {
		t.Fatalf("rewriteImports: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, `old "`+corporateModulePath+`/subpackage"`) {
		t.Fatalf("internal import was not updated:\n%s", text)
	}
	if !strings.Contains(text, `"fmt"`) || !strings.Contains(text, "// profiler-probe is documentation.") {
		t.Fatalf("unrelated content changed:\n%s", text)
	}
}

func TestRepositorySmokeImportMatchesCurrentModule(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	module, err := currentModulePath(root)
	if err != nil {
		t.Fatalf("current module: %v", err)
	}
	smoke := readTestFile(t, filepath.Join(root, "cmd", "pyroscope-smoke", "main.go"))
	if !strings.Contains(smoke, `profiler "`+module+`"`) {
		t.Fatalf("smoke command does not import current module %q; use go run ./cmd/set-module-path", module)
	}
}

func newFixture(t *testing.T, modulePath, smokeImport string, valid bool) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	writeTestFile(t, filepath.Join(root, "go.mod"), "module "+modulePath+"\n\ngo 1.25.0\n")
	writeTestFile(t, filepath.Join(root, "value.go"), "package profilerprobe\n\nconst Value = 1\n")
	writeTestFile(t, filepath.Join(root, "internal", "value", "value.go"), "package value\n\nconst Number = 1\n")
	writeTestFile(t, filepath.Join(root, "internal", "consumer", "consumer.go"), "package consumer\n\nimport \""+smokeImport+"/internal/value\"\n\nvar Number = value.Number\n")
	smokeSource := "//go:build smoke\n\npackage main\n\nimport probe \"" + smokeImport + "\"\n\nfunc main() { _ = probe.Value }\n"
	if !valid {
		smokeSource = "//go:build smoke\n\npackage main\n\nimport probe \"" + smokeImport + "\"\n\nfunc main() { _ = probe.DoesNotExist }\n"
	}
	writeTestFile(t, filepath.Join(root, "cmd", "pyroscope-smoke", "main.go"), smokeSource)
	return root
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func assertContains(t *testing.T, path, expected string) {
	t.Helper()
	if content := readTestFile(t, path); !strings.Contains(content, expected) {
		t.Fatalf("%s does not contain %q:\n%s", path, expected, content)
	}
}

func assertNotContains(t *testing.T, path, unexpected string) {
	t.Helper()
	if content := readTestFile(t, path); strings.Contains(content, unexpected) {
		t.Fatalf("%s unexpectedly contains %q:\n%s", path, unexpected, content)
	}
}
