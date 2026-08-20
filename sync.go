//go:build ignore

// Command sync re-exports the TypeScript compiler's internal packages under a
// public import path.
//
// Upstream keeps everything under internal/, which Go forbids other modules
// from importing. This moves internal/ to use-at-your-own-risk/ and rewrites
// import paths to match, producing a module that can be depended on normally.
// The Go code is upstream's, unmodified apart from those paths.
//
// Run from the repository root after syncing upstream:
//
//	go run sync.go
//
// The target module path is read from go.mod, so a fork needs no edit here.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	upstreamModule = "github.com/microsoft/typescript-go"
	sourceDir      = "internal"
	targetDir      = "use-at-your-own-risk"
)

// pruned are upstream trees this fork does not ship.
//
// The fork exists so other modules can import the compiler's packages. Nothing
// else in the repository serves that purpose, and everything here is paid for
// by every consumer: Go downloads the whole module zip, so upstream's test
// corpus would land in every user's module cache.
//
// testdata alone is 28,000 files and 85 MB of conformance baselines. Dropping
// these takes the module from ~33,600 files to ~650.
//
// Nothing shipped embeds any of this. The only //go:embed directives in the
// re-exported packages point at the diagnostics locale data, which lives
// inside the package and is copied verbatim.
var pruned = []string{
	"testdata",     // upstream's conformance suite and baselines
	"_submodules",  // the TypeScript repo, as a submodule
	"_extension",   // the VS Code extension
	"_tools",       // upstream's build tooling
	".vscode",
	".devcontainer",
	"node_modules",
}

// pruneTests also drops *_test.go from the re-exported packages. They are
// another 4,500 files, they reference the testdata removed above, and a
// consumer never runs a dependency's tests.
var pruneTests = flag.Bool("prune-tests", true, "drop *_test.go from the re-exported packages")

var dryRun = flag.Bool("n", false, "report what would change without writing")

func main() {
	flag.Parse()

	target, err := modulePath()
	if err != nil {
		fatal(err)
	}
	if target == upstreamModule {
		fatal(fmt.Errorf("go.mod still declares the upstream module path; a fork must rename it"))
	}
	fmt.Printf("rewriting %s/%s -> %s/%s\n", upstreamModule, sourceDir, target, targetDir)

	if _, err := os.Stat(sourceDir); err != nil {
		fatal(fmt.Errorf("no %s/ directory: run this from the repository root, after syncing upstream", sourceDir))
	}

	pruneCount, err := prune()
	if err != nil {
		fatal(err)
	}

	moved, copied, err := relocate(target)
	if err != nil {
		fatal(err)
	}
	// Every non-Go file matters. The diagnostics package embeds one gzipped
	// JSON file per locale, and dropping those makes the package fail to
	// build with "pattern loc/cs-CZ.json.gz: no matching files found" — which
	// then breaks every consumer, since ast imports diagnostics.
	if copied == 0 {
		fatal(fmt.Errorf("no non-Go files were copied; embedded assets are almost certainly missing"))
	}

	rewritten, err := rewriteRemaining(target)
	if err != nil {
		fatal(err)
	}

	testsDropped := 0
	if *pruneTests {
		testsDropped, err = dropTests()
		if err != nil {
			fatal(err)
		}
	}

	fmt.Printf("\n%d Go files relocated, %d asset files copied, %d files rewritten elsewhere\n",
		moved, copied, rewritten)
	fmt.Printf("%d files pruned, %d test files dropped\n", pruneCount, testsDropped)
	if *dryRun {
		fmt.Println("(dry run: nothing was written)")
	}
}

// prune removes the upstream trees this fork does not ship.
func prune() (int, error) {
	total := 0
	for _, dir := range pruned {
		n, err := countFiles(dir)
		if err != nil {
			return total, err
		}
		if n == 0 {
			continue
		}
		total += n
		fmt.Printf("  pruning %s (%d files)\n", dir, n)
		if *dryRun {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return total, fmt.Errorf("pruning %s: %w", dir, err)
		}
	}
	return total, nil
}

// dropTests removes test files from the re-exported packages.
func dropTests() (int, error) {
	count := 0
	err := filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		name := info.Name()
		if !strings.HasSuffix(name, "_test.go") && name != "testdata" {
			return nil
		}
		count++
		if *dryRun {
			return nil
		}
		return os.Remove(path)
	})
	if err != nil {
		return count, err
	}
	// Package-local testdata directories go too; they exist only for the tests
	// just removed.
	_ = filepath.Walk(targetDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || info.Name() != "testdata" {
			return nil
		}
		n, _ := countFiles(path)
		count += n
		if !*dryRun {
			_ = os.RemoveAll(path)
		}
		return filepath.SkipDir
	})
	return count, nil
}

func countFiles(dir string) (int, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return 0, nil
	}
	n := 0
	err = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			n++
		}
		return nil
	})
	return n, err
}

// modulePath reads the module path from go.mod.
func modulePath() (string, error) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(b)
	if m == nil {
		return "", fmt.Errorf("no module line in go.mod")
	}
	return string(m[1]), nil
}

// relocate moves internal/ to use-at-your-own-risk/, rewriting import paths in
// Go files and copying everything else byte for byte.
func relocate(target string) (moved, copied int, err error) {
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// filepath.Walk yields backslashes on Windows, so normalize before
		// trimming the prefix and convert back when rebuilding the path.
		rel := strings.TrimPrefix(filepath.ToSlash(path), sourceDir+"/")
		dst := filepath.Join(targetDir, filepath.FromSlash(rel))

		if *dryRun {
			if strings.HasSuffix(path, ".go") {
				moved++
			} else {
				copied++
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		if strings.HasSuffix(path, ".go") {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dst, rewriteImports(b, target), info.Mode()); err != nil {
				return err
			}
			moved++
		} else {
			// Assets are copied verbatim: rewriting them would corrupt
			// binary content such as the gzipped locale data.
			if err := copyFile(path, dst, info.Mode()); err != nil {
				return err
			}
			copied++
		}
		return os.Remove(path)
	})
	return moved, copied, err
}

// rewriteRemaining fixes import paths in the rest of the repository, so that
// cmd/ and the tooling still build after the move.
func rewriteRemaining(target string) (int, error) {
	count := 0
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", targetDir, "_submodules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := rewriteImports(b, target)
		if string(out) == string(b) {
			return nil
		}
		count++
		if *dryRun {
			return nil
		}
		return os.WriteFile(path, out, info.Mode())
	})
	return count, err
}

func rewriteImports(b []byte, target string) []byte {
	s := string(b)
	s = strings.ReplaceAll(s, `"`+upstreamModule+`/`+sourceDir+`/`, `"`+target+`/`+targetDir+`/`)
	s = strings.ReplaceAll(s, `"`+upstreamModule+`/`+sourceDir+`"`, `"`+target+`/`+targetDir+`"`)
	return []byte(s)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sync:", err)
	os.Exit(1)
}