package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// TestReleaseAssetName is the regression guard tying update.go to the
// "Package archive" naming convention in .github/workflows/release.yml.
// The expected names are the actual published v0.1.5 release assets.
func TestReleaseAssetName(t *testing.T) {
	cases := []struct {
		tag, goos, goarch string
		want              string
	}{
		// Real v0.1.5 asset names.
		{"v0.1.5", "darwin", "arm64", "grafel_0.1.5_macos_arm64.tar.gz"},
		{"v0.1.5", "darwin", "amd64", "grafel_0.1.5_macos_x86_64.tar.gz"},
		{"v0.1.5", "linux", "arm64", "grafel_0.1.5_linux_arm64.tar.gz"},
		{"v0.1.5", "linux", "amd64", "grafel_0.1.5_linux_x86_64.tar.gz"},
		{"v0.1.5", "windows", "amd64", "grafel_0.1.5_windows_x86_64.zip"},
		// Tag without a leading "v" is handled identically.
		{"0.1.5", "linux", "amd64", "grafel_0.1.5_linux_x86_64.tar.gz"},
	}
	for _, c := range cases {
		got := releaseAssetName(c.tag, c.goos, c.goarch)
		if got != c.want {
			t.Errorf("releaseAssetName(%q,%q,%q) = %q, want %q",
				c.tag, c.goos, c.goarch, got, c.want)
		}
	}
}

// TestExtractTarGzMember builds an in-memory .tar.gz containing a fake grafel
// binary plus a LICENSE, then asserts only the binary lands at destPath with
// the exec bit set.
func TestExtractTarGzMember(t *testing.T) {
	const binContent = "#!/bin/sh\necho fake-grafel\n"

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	writeTar := func(name, body string, mode int64) {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}
	writeTar("LICENSE", "MIT...", 0o644)
	writeTar("grafel", binContent, 0o755)
	writeTar("README.md", "# grafel", 0o644)
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "grafel_0.1.5_linux_x86_64.tar.gz")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	destPath := filepath.Join(dir, "grafel")
	if err := extractTarGzMember(archivePath, "grafel", destPath); err != nil {
		t.Fatalf("extractTarGzMember: %v", err)
	}

	assertExtractedBinary(t, destPath, binContent)
}

// TestExtractZipMember builds an in-memory .zip containing a fake grafel.exe
// plus a LICENSE, then asserts only the binary lands at destPath with the exec
// bit set.
func TestExtractZipMember(t *testing.T) {
	const binContent = "MZ fake-windows-binary"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZip := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	writeZip("LICENSE", "MIT...")
	writeZip("grafel.exe", binContent)
	writeZip("README.md", "# grafel")
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "grafel_0.1.5_windows_x86_64.zip")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	destPath := filepath.Join(dir, "grafel.exe")
	if err := extractZipMember(archivePath, "grafel.exe", destPath); err != nil {
		t.Fatalf("extractZipMember: %v", err)
	}

	assertExtractedBinary(t, destPath, binContent)
}

func assertExtractedBinary(t *testing.T, destPath, wantContent string) {
	t.Helper()

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if string(got) != wantContent {
		t.Errorf("extracted content = %q, want %q", string(got), wantContent)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("stat extracted binary: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("extracted binary not executable: mode = %v", info.Mode())
	}

	// Only the binary should be at destPath's directory under the binary name —
	// LICENSE/README must not have been written alongside it by the extractor.
	dir := filepath.Dir(destPath)
	for _, junk := range []string{"LICENSE", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, junk)); err == nil {
			t.Errorf("extractor leaked non-binary member %q to dest dir", junk)
		}
	}
}
