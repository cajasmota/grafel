package install

import (
	"fmt"
	"strings"
	"testing"
)

// realisticReleaseJSON mirrors the shape of a real GitHub Releases API
// response for a tag: each asset carries a sizeable nested "uploader" object
// sitting BETWEEN its "name" and "browser_download_url" fields. This is the
// exact shape that broke the old naive proximity scanner, which looked for
// browser_download_url within a fixed window after the asset name.
func realisticReleaseJSON(tag string) string {
	version := strings.TrimPrefix(tag, "v")
	asset := func(id int, name string) string {
		return fmt.Sprintf(`{
      "url": "https://api.github.com/repos/cajasmota/grafel/releases/assets/%d",
      "id": %d,
      "node_id": "RA_kwDOABCDEF%d",
      "name": "%s",
      "label": "",
      "uploader": {
        "login": "cajasmota",
        "id": 123456,
        "node_id": "MDQ6VXNlcjEyMzQ1Ng==",
        "avatar_url": "https://avatars.githubusercontent.com/u/123456?v=4",
        "gravatar_id": "",
        "url": "https://api.github.com/users/cajasmota",
        "html_url": "https://github.com/cajasmota",
        "type": "User",
        "site_admin": false
      },
      "content_type": "application/gzip",
      "state": "uploaded",
      "size": 12345678,
      "download_count": 7,
      "created_at": "2026-06-23T00:00:00Z",
      "updated_at": "2026-06-23T00:00:00Z",
      "browser_download_url": "https://github.com/cajasmota/grafel/releases/download/%s/%s"
    }`, id, id, id, name, tag, name)
	}

	return fmt.Sprintf(`{
  "url": "https://api.github.com/repos/cajasmota/grafel/releases/12345",
  "tag_name": "%s",
  "name": "%s",
  "draft": false,
  "prerelease": false,
  "assets": [
    %s,
    %s,
    %s
  ],
  "body": "release notes"
}`,
		tag, tag,
		asset(1, fmt.Sprintf("grafel_%s_macos_arm64.tar.gz", version)),
		asset(2, fmt.Sprintf("grafel_%s_windows_x86_64.zip", version)),
		asset(3, "checksums.txt"),
	)
}

func TestFindAssetDownloadURL(t *testing.T) {
	const tag = "v0.1.5.1"
	body := realisticReleaseJSON(tag)

	t.Run("dotted-name asset resolves", func(t *testing.T) {
		want := "https://github.com/cajasmota/grafel/releases/download/v0.1.5.1/grafel_0.1.5.1_macos_arm64.tar.gz"
		got, err := findAssetDownloadURL(body, "grafel_0.1.5.1_macos_arm64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("download URL mismatch:\n got = %q\nwant = %q", got, want)
		}
	})

	t.Run("windows zip asset resolves", func(t *testing.T) {
		want := "https://github.com/cajasmota/grafel/releases/download/v0.1.5.1/grafel_0.1.5.1_windows_x86_64.zip"
		got, err := findAssetDownloadURL(body, "grafel_0.1.5.1_windows_x86_64.zip")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("download URL mismatch:\n got = %q\nwant = %q", got, want)
		}
	})

	t.Run("missing asset errors cleanly", func(t *testing.T) {
		_, err := findAssetDownloadURL(body, "grafel_0.1.5.1_linux_arm64.tar.gz")
		if err == nil {
			t.Fatal("expected an error for an absent asset, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("error should mention the asset is not found, got: %v", err)
		}
	})

	t.Run("malformed JSON errors cleanly", func(t *testing.T) {
		if _, err := findAssetDownloadURL("{not json", "anything"); err == nil {
			t.Fatal("expected a parse error for malformed JSON, got nil")
		}
	})
}
