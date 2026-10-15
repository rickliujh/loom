package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/internal/proc"
	"github.com/rickliujh/loom/internal/server"
	"github.com/spf13/cobra"
)

var (
	serveListen            string
	serveRoots             []string
	serveAllowOutsideRoots bool
	serveNoRemoteModules   bool
	serveAllowRemote       bool
	serveAllowedHosts      []string
	serveTokenFile         string
	serveStateDir          string
	serveHistory           string
	serveMaxConcurrentJobs int
	serveOpen              bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the web UI and its API",
	Long: `Start a local web server with a UI for browsing, editing, inspecting, diffing
and running loom modules.

The server binds 127.0.0.1 by default and prints one URL. Open it: the token
in its fragment signs the browser in. Every API call needs that token, which
changes each time the server starts.

Modules are found under the --root directories (default: the current
directory). Jobs — runs, diffs, generate and bulk — execute through the same
code as the matching commands, one at a time unless --max-concurrent-jobs
says otherwise, and their history is kept in the state directory.

Stop the server with Ctrl-C: running jobs are cancelled, given ten seconds to
stop, and recorded as interrupted if they have not.`,
	Args: cobra.NoArgs,
	RunE: runServe,
}

func init() {
	f := serveCmd.Flags()
	f.StringVar(&serveListen, "listen", "127.0.0.1:7788", "Address to listen on; port 0 picks a free port. A non-loopback address needs --allow-remote")
	f.StringArrayVar(&serveRoots, "root", nil, "Directory whose modules the UI works with (can be repeated; default: current directory)")
	f.BoolVar(&serveAllowOutsideRoots, "allow-outside-roots", false, "Accept local module sources outside the roots")
	f.BoolVar(&serveNoRemoteModules, "no-remote-modules", false, "Refuse module sources that are git URLs")
	f.BoolVar(&serveAllowRemote, "allow-remote", false, "Allow listening on a non-loopback address (plain HTTP: prefer an SSH tunnel)")
	f.StringArrayVar(&serveAllowedHosts, "allowed-host", nil, "Extra Host header value to accept, as name or name:port (can be repeated)")
	f.StringVar(&serveTokenFile, "token-file", "", "Write the session token to this file (mode 0600) instead of printing it")
	f.StringVar(&serveStateDir, "state-dir", "", "Directory for job history, presets and workspaces (default: $XDG_STATE_HOME/loom/serve)")
	f.StringVar(&serveHistory, "history", server.HistoryDisk, "Where job history is kept: disk (survives restarts) or memory")
	f.IntVar(&serveMaxConcurrentJobs, "max-concurrent-jobs", 1, "Executing jobs that may run at once")
	f.BoolVar(&serveOpen, "open", false, "Open the UI in a browser")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	listen, err := serveListenAddr(serveListen, serveAllowRemote)
	if err != nil {
		return err
	}
	if serveHistory != server.HistoryDisk && serveHistory != server.HistoryMemory {
		return fmt.Errorf("invalid --history %q, expected disk or memory", serveHistory)
	}
	if serveMaxConcurrentJobs < 1 {
		return fmt.Errorf("--max-concurrent-jobs must be at least 1")
	}
	roots := serveRoots
	if len(roots) == 0 {
		roots = []string{"."}
	}
	stateDir := serveStateDir
	if stateDir == "" {
		if stateDir, err = defaultStateDir(); err != nil {
			return err
		}
	}

	// Server mode: every subprocess from here on gets its own session, no
	// terminal to prompt on, and dies with its job.
	proc.EnableIsolation()

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer ln.Close()

	srv, err := server.New(server.Config{
		Addr:              ln.Addr().String(),
		Roots:             roots,
		AllowOutsideRoots: serveAllowOutsideRoots,
		NoRemoteModules:   serveNoRemoteModules,
		AllowedHosts:      serveAllowedHosts,
		StateDir:          stateDir,
		History:           serveHistory,
		MaxConcurrentJobs: serveMaxConcurrentJobs,
		Version:           resolveVersion(),
		Docs:              Docs,
		Logger:            newLogger(),
	})
	if err != nil {
		return err
	}

	out := cmd.ErrOrStderr()
	if !server.IsLoopbackListen(listen) {
		prettylog.Warningf(out, "listening on %s over plain HTTP: the token and everything the UI shows cross the network unencrypted. Prefer an SSH tunnel to a loopback address.", ln.Addr())
	}
	if serveTokenFile != "" {
		if err := writeTokenFile(serveTokenFile, srv.Token()); err != nil {
			return err
		}
		fmt.Fprintf(out, "loom serve listening on http://%s/\n", ln.Addr())
		fmt.Fprintf(out, "session token written to %s; open http://%s/#token=<token>\n", serveTokenFile, ln.Addr())
	} else {
		fmt.Fprintf(out, "loom serve listening; open this URL to sign in:\n\n  %s\n\n", srv.URL())
	}
	fmt.Fprintf(out, "state directory: %s\n", stateDir)

	if serveOpen {
		openBrowser(srv.URL(), out)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.Serve(ctx, ln); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	fmt.Fprintln(out, "loom serve stopped")
	return nil
}

// serveListenAddr checks the listen address. A missing host means loopback,
// not every interface, and anything but loopback needs --allow-remote.
func serveListenAddr(addr string, allowRemote bool) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid --listen %q: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	addr = net.JoinHostPort(host, port)
	if !server.IsLoopbackListen(addr) && !allowRemote {
		return "", fmt.Errorf("--listen %s is not a loopback address: anyone who can reach it could try the token, and HTTP is unencrypted. Pass --allow-remote to do it anyway, or keep the default and use an SSH tunnel", addr)
	}
	return addr, nil
}

// defaultStateDir is $XDG_STATE_HOME/loom/serve, or ~/.local/state/loom/serve.
func defaultStateDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "loom", "serve"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the state directory: %w; pass --state-dir", err)
	}
	return filepath.Join(home, ".local", "state", "loom", "serve"), nil
}

// writeTokenFile writes the token readable by its owner only, tightening an
// existing file's mode before any secret goes into it.
func writeTokenFile(path, token string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("writing token file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("writing token file: %w", err)
	}
	if _, err := io.WriteString(f, token+"\n"); err != nil {
		f.Close()
		return fmt.Errorf("writing token file: %w", err)
	}
	return f.Close()
}

// openBrowser opens url with the desktop's opener, best effort: a server
// with no desktop still starts, and says where to go.
func openBrowser(url string, out io.Writer) {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "linux", "freebsd", "openbsd", "netbsd":
		name = "xdg-open"
	default:
		fmt.Fprintln(out, "--open is not supported on this system; open the URL above")
		return
	}
	c := proc.Command(context.Background(), name, url)
	if err := c.Start(); err != nil {
		fmt.Fprintf(out, "could not open a browser (%v); open the URL above\n", err)
		return
	}
	go func() { _ = c.Wait() }()
}
