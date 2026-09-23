package recipe

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// ghidraScript is a Java GhidraScript written next to MorgueExport.java and run
// by analyzeHeadless. Name must equal the public class name + ".java".
type ghidraScript struct {
	Name   string
	Source string
}

// nativeAOTTool is the tools-registry name of washi1337/ghidra-nativeaot.
const nativeAOTTool = "ghidra-nativeaot"

// nativeAOTAnalyzer is the analyzer name the extension registers
// (nativeaot.Constants.NAME). It is disabled by default, so a pre-script
// turns it on before auto-analysis.
const nativeAOTAnalyzer = "Native AOT Analyzer"

var nativeAOTPreScript = ghidraScript{
	Name: "MorgueNativeAot.java",
	Source: `// MorgueNativeAot.java — enable the ghidra-nativeaot analyzer before auto-analysis
import ghidra.app.script.GhidraScript;

public class MorgueNativeAot extends GhidraScript {
    @Override
    public void run() throws Exception {
        String name = "` + nativeAOTAnalyzer + `";
        if (!getCurrentAnalysisOptionsAndValues(currentProgram).containsKey(name)) {
            System.out.println("Morgue:pre:NativeAOT analyzer not registered - extension not loaded");
            return;
        }
        setAnalysisOption(currentProgram, name, "true");
        System.out.println("Morgue:pre:NativeAOT metadata recovery enabled (" + name + ")");
    }
}
`,
}

// prepareNativeAOT makes the ghidra-nativeaot extension available to the Ghidra
// at ghidraToolPath (downloading it on demand) and returns the pre-script that
// enables its analyzer. Upstream only ships builds for specific Ghidra versions
// and Ghidra breaks binary compatibility between minor releases (12.1 changed
// DataTypeComponent.setFieldName's return type -> NoSuchMethodError that aborts
// the whole headless run), so when the versions differ the extension jar is
// recompiled from the bundled sources against the installed Ghidra. Any
// failure is returned so the caller can fall back to plain Ghidra analysis.
func prepareNativeAOT(ctx context.Context, m *tools.Manager, ghidraToolPath, javaPath string, log func(string)) (ghidraScript, error) {
	if m == nil {
		return ghidraScript{}, fmt.Errorf("no tools manager")
	}
	if !m.IsInstalled(nativeAOTTool) {
		log("Downloading ghidra-nativeaot extension")
		if _, err := m.Install(ctx, nativeAOTTool, nil); err != nil {
			return ghidraScript{}, fmt.Errorf("install %s: %w", nativeAOTTool, err)
		}
	}
	jar, err := m.Resolve(nativeAOTTool)
	if err != nil {
		return ghidraScript{}, err
	}
	// Extension layout: <ext>/lib/ghidra-nativeaot.jar + <ext>/extension.properties.
	extDir := filepath.Dir(filepath.Dir(jar))
	ghidraHome := filepath.Dir(ghidraToolPath)
	ghidraVer := readProperty(filepath.Join(ghidraHome, "Ghidra", "application.properties"), "application.version")
	extVer := readProperty(filepath.Join(extDir, "extension.properties"), "version")
	if ghidraVer == "" {
		return ghidraScript{}, fmt.Errorf("cannot read Ghidra version under %s", ghidraHome)
	}

	// Ghidra loads unpacked extensions from <install>/Ghidra/Extensions/<name>.
	dst := filepath.Join(ghidraHome, "Ghidra", "Extensions", nativeAOTTool)
	marker := filepath.Join(dst, ".morgue-build")
	stamp := fmt.Sprintf("%s ext=%s ghidra=%s", m.Check(nativeAOTTool).Version, extVer, ghidraVer)
	if b, rerr := os.ReadFile(filepath.Clean(marker)); rerr == nil && string(b) == stamp {
		return nativeAOTPreScript, nil
	}

	_ = os.RemoveAll(dst)
	if err := copyDir(extDir, dst); err != nil {
		_ = os.RemoveAll(dst)
		return ghidraScript{}, fmt.Errorf("install extension into Ghidra: %w", err)
	}
	if extVer != ghidraVer {
		log(fmt.Sprintf("ghidra-nativeaot is built for Ghidra %s, installed Ghidra is %s — recompiling it from source", extVer, ghidraVer))
		srcZip := filepath.Join(extDir, "lib", "ghidra-nativeaot-src.zip")
		if err := rebuildExtensionJar(ctx, srcZip, jar, filepath.Join(dst, "lib", filepath.Base(jar)), ghidraHome, javaPath); err != nil {
			_ = os.RemoveAll(dst)
			return ghidraScript{}, fmt.Errorf("recompile for Ghidra %s: %w", ghidraVer, err)
		}
	}
	if err := os.WriteFile(marker, []byte(stamp), 0644); err != nil {
		return ghidraScript{}, err
	}
	return nativeAOTPreScript, nil
}

// rebuildExtensionJar compiles the extension sources in srcZip against the
// Ghidra jars under ghidraHome and writes outJar: the original jar's resources
// (manifest, help, images) plus the freshly compiled classes.
func rebuildExtensionJar(ctx context.Context, srcZip, origJar, outJar, ghidraHome, javaPath string) error {
	javac, err := findJavac(javaPath)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "morgue-naot-build-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	srcDir := filepath.Join(work, "src")
	classDir := filepath.Join(work, "classes")
	sources, err := unzipJava(srcZip, srcDir)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return fmt.Errorf("no .java sources in %s", srcZip)
	}

	// Classpath: every Ghidra module jar (Ghidra/**/lib/*.jar), minus installed
	// extensions. Passed via an @argfile — 150+ jars overflow the Windows
	// command-line limit. javac argfiles treat '\' as an escape: use '/'.
	extRoot := filepath.Join(ghidraHome, "Ghidra", "Extensions")
	var cp []string
	_ = filepath.WalkDir(filepath.Join(ghidraHome, "Ghidra"), func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil //nolint:nilerr // unreadable entry: skip it, keep collecting the rest
		}
		if d.IsDir() && p == extRoot {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".jar") && filepath.Base(filepath.Dir(p)) == "lib" {
			cp = append(cp, filepath.ToSlash(p))
		}
		return nil
	})
	quote := func(s string) string { return `"` + filepath.ToSlash(s) + `"` }
	lines := []string{"-nowarn", "-proc:none", "-encoding", "UTF-8", "-d", quote(classDir),
		"-cp", quote(strings.Join(cp, string(os.PathListSeparator)))}
	for _, s := range sources {
		lines = append(lines, quote(s))
	}
	argFile := filepath.Join(work, "javac.args")
	if err := os.WriteFile(argFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return err
	}
	res, err := util.RunCmd(ctx, javac, []string{"@" + argFile}, work)
	if err != nil || res == nil || res.ExitCode != 0 {
		detail := ""
		if res != nil {
			detail = strings.TrimSpace(res.Stderr + "\n" + res.Stdout)
			if len(detail) > 800 {
				detail = detail[:800]
			}
		}
		if err == nil && res != nil {
			err = fmt.Errorf("exit status %d", res.ExitCode)
		} else if err == nil {
			err = fmt.Errorf("no result")
		}
		return fmt.Errorf("javac failed (%w): %s", err, detail)
	}
	return writeRebuiltJar(origJar, classDir, outJar)
}

// findJavac locates javac next to javaPath, then via JAVA_HOME and PATH.
// Ghidra itself needs a JDK, so one is present wherever Ghidra runs.
func findJavac(javaPath string) (string, error) {
	name := "javac"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var candidates []string
	if javaPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(javaPath), name))
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, "bin", name))
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("javac"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("javac not found (a JDK is required to recompile the extension)")
}

// unzipJava extracts the .java entries of zipPath into dst and returns their paths.
func unzipJava(zipPath, dst string) ([]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	var out []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".java") {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(f.Name))
		if rel, rerr := filepath.Rel(dst, target); rerr != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("unsafe path in %s: %s", zipPath, f.Name)
		}
		if err := extractZipFile(f, target); err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	return out, nil
}

func extractZipFile(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	w, err := os.Create(filepath.Clean(target))
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil { //nolint:gosec // G110: trusted pinned release asset
		_ = w.Close()
		return err
	}
	return w.Close()
}

// writeRebuiltJar writes outJar with every non-.class entry of origJar plus all
// .class files under classDir.
func writeRebuiltJar(origJar, classDir, outJar string) error {
	zr, err := zip.OpenReader(origJar)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	tmp := outJar + ".tmp"
	f, err := os.Create(filepath.Clean(tmp))
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	fail := func(e error) error {
		_ = zw.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return e
	}
	for _, e := range zr.File {
		// Directory entries are optional in a jar (and zip.Writer.Copy rejects them).
		if strings.HasSuffix(e.Name, ".class") || strings.HasSuffix(e.Name, "/") {
			continue
		}
		if err := zw.Copy(e); err != nil {
			return fail(err)
		}
	}
	root, err := os.OpenRoot(classDir)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = root.Close() }()
	classes := root.FS()
	werr := fs.WalkDir(classes, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(classes, p)
		if err != nil {
			return err
		}
		w, err := zw.Create(p) // fs paths are already '/'-separated
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if werr != nil {
		return fail(werr)
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, outJar)
}

// readProperty returns the value of key in a Java .properties file ("" if absent).
func readProperty(path, key string) string {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// copyDir recursively copies src into dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}
