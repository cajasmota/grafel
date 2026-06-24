//go:build windows

package dashboard

import "testing"

// TestIsDriveRoot exercises the drive-root detection that decides when "up"
// should ascend to the drives level (the Windows-only fix for the picker being
// stuck on C:). Table-driven; Windows-guarded since isDriveRoot is too.
func TestIsDriveRoot(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`C:\`, true},
		{`D:\`, true},
		{`c:\`, true},
		{`Z:/`, true},
		{`C:`, true},
		{`C:\Users`, false},
		{`C:\Users\foo`, false},
		{`\\server\share`, false},
		{``, false},
		{`/`, false},
		{`12`, false},
	}
	for _, c := range cases {
		if got := isDriveRoot(c.in); got != c.want {
			t.Errorf("isDriveRoot(%q) = %v; want %v", c.in, got, c.want)
		}
	}
}

// TestListDrives sanity-checks that at least one drive is discovered on a real
// Windows host and that every returned entry is a valid drive root.
func TestListDrives(t *testing.T) {
	drives := listDrives()
	if len(drives) == 0 {
		t.Fatal("listDrives returned no drives on a Windows host")
	}
	for _, d := range drives {
		if !isDriveRoot(d.Path) {
			t.Errorf("drive Path %q is not a drive root", d.Path)
		}
		if !d.IsDir {
			t.Errorf("drive %q IsDir = false", d.Name)
		}
	}
}
