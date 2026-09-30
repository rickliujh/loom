package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// SV1: loopback unless told otherwise. `loom serve` itself is not started
// here: it switches the whole process into subprocess isolation, which would
// leak into every other test of this package.
func TestServe_SV1_LoopbackByDefault(t *testing.T) {
	if got := serveCmd.Flags().Lookup("listen").DefValue; got != "127.0.0.1:7788" {
		t.Errorf("default --listen = %q", got)
	}
	cases := []struct {
		addr        string
		allowRemote bool
		want        string
		wantErr     bool
	}{
		{"127.0.0.1:7788", false, "127.0.0.1:7788", false},
		{"localhost:0", false, "localhost:0", false},
		{"[::1]:7788", false, "[::1]:7788", false},
		// A missing host is loopback, not every interface.
		{":0", false, "127.0.0.1:0", false},
		{"0.0.0.0:7788", false, "", true},
		{"[::]:7788", false, "", true},
		{"192.168.1.5:7788", false, "", true},
		{"box.lan:7788", false, "", true},
		{"0.0.0.0:7788", true, "0.0.0.0:7788", false},
		{"no-port", false, "", true},
	}
	for _, tc := range cases {
		got, err := serveListenAddr(tc.addr, tc.allowRemote)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("serveListenAddr(%q, %v) = %q, %v", tc.addr, tc.allowRemote, got, err)
		}
	}
}

func TestServe_TokenFileIsPrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "token")
	// An existing, readable file is tightened before the token goes in.
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTokenFile(p, "abc"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v, want 0600", info.Mode().Perm())
	}
	if b, _ := os.ReadFile(p); string(b) != "abc\n" {
		t.Errorf("token file = %q", b)
	}
}

func TestServe_DefaultStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	if d, _ := defaultStateDir(); d != filepath.Join("/x/state", "loom", "serve") {
		t.Errorf("with XDG_STATE_HOME: %s", d)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	if d, _ := defaultStateDir(); runtime.GOOS != "windows" && d != filepath.Join("/home/u", ".local", "state", "loom", "serve") {
		t.Errorf("without XDG_STATE_HOME: %s", d)
	}
}
