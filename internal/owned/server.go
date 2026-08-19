// Package owned starts this project's own Go server for a test to connect to.
//
// It is the other half of the compatibility matrix. The vanilla lanes ask the
// game what it does; this one asks whether the server this project ships
// agrees, using the same client, the same scenarios, and the same assertions.
// A row that only ever ran against vanilla says nothing about the server, and
// a server only ever driven by its own tests says nothing about the client.
//
// What it cannot find is stated rather than hidden: both ends of this lane are
// this project's code, so a mutual misunderstanding of the protocol passes it.
// The vanilla lanes and the Node interop lane are what catch one.
//
// The binary is never built here. A lane that compiled a sibling module during
// an ordinary test run would be slow, would need that module's toolchain, and
// would reach into a checkout by path — which is exactly what M10 Task 4
// stopped the vanilla lanes doing. It is named by GOTHEFTCRAFT_SERVER, and
// when that is unset the caller skips.
//
// The server runs in offline mode, which is a limitation of this whole lane
// and not a property of the server: nothing measured here says anything about
// online-mode behaviour.
package owned

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ErrServer reports a server that could not be started or reached.
var ErrServer = errors.New("owned: server")

// BinaryVariable names the environment variable holding the server binary.
const BinaryVariable = "GOTHEFTCRAFT_SERVER"

// readyLine is what the server writes once it is listening.
const readyLine = "server started"

// Options configures one server.
type Options struct {
	// Binary is the server to run. When empty it comes from GOTHEFTCRAFT_SERVER.
	Binary string
	// Port is what it listens on. Zero picks a free one, which is what lets two
	// tests run at once.
	Port int
	// Seed decides the world, and defaults to a fixed one: a scenario that
	// walks in a straight line should walk over the same blocks every run.
	Seed int64
	// Generator names the world type. Empty means the server's own default.
	Generator string
	// Radius is how much world to pre-generate, in chunks. The default is
	// small on purpose — a lane needs the ground under one player, not a map.
	Radius int
	// Ready is how long to wait for the server to announce itself.
	Ready time.Duration
}

// Server is a running server and the log it has written.
type Server struct {
	cmd  *exec.Cmd
	port int
	dir  string

	mu    sync.Mutex
	lines []string
	// stopped guards against two shutdowns racing, which a cleanup and a
	// failing test can otherwise do at once.
	stopped bool
}

// Start runs the server and waits for it to announce itself.
//
// It skips the test when no binary is named, and registers its own shutdown:
// a suite that leaked a server per run would be turned off by whoever noticed.
func Start(t *testing.T, options Options) *Server {
	t.Helper()

	binary := options.Binary
	if binary == "" {
		binary = os.Getenv(BinaryVariable)
	}
	if binary == "" {
		t.Skipf("no owned server binary: set %s to one built with "+
			"`cd examples && go build -o <path> ./vanilla` in the server repository",
			BinaryVariable)
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatalf("resolve the binary path: %v", err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("no owned server binary at %s: %v", binary, err)
	}

	if options.Port == 0 {
		options.Port = freePort(t)
	}
	if options.Ready == 0 {
		options.Ready = time.Minute
	}
	if options.Radius == 0 {
		options.Radius = 8
	}

	dir := t.TempDir()
	arguments := []string{
		"-data-dir", dir,
		"-port", strconv.Itoa(options.Port),
		"-online-mode=false",
		"-seed", strconv.FormatInt(options.Seed, 10),
		"-world-radius", strconv.Itoa(options.Radius),
	}
	if options.Generator != "" {
		arguments = append(arguments, "-generator", options.Generator)
	}

	cmd := exec.Command(binary, arguments...)
	cmd.Dir = dir

	// The server logs through slog to stderr; both streams are read as one so
	// a panic and a log line keep their order.
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("capture the server's output: %v", err)
	}
	cmd.Stderr = cmd.Stdout

	server := &Server{cmd: cmd, port: options.Port, dir: dir}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the server: %v", err)
	}
	t.Cleanup(server.Stop)

	ready := make(chan struct{})
	go server.read(output, ready)

	select {
	case <-ready:
	case <-time.After(options.Ready):
		t.Fatalf("the server did not become ready in %s\n%s", options.Ready, server.Log())
	}

	// Announcing and accepting are two different moments, and the log records
	// the first.
	if err := server.waitForPort(options.Ready); err != nil {
		t.Fatalf("%v\n%s", err, server.Log())
	}

	return server
}

// Addr returns the address to connect to.
func (s *Server) Addr() string { return "127.0.0.1:" + strconv.Itoa(s.port) }

// Dir returns the server's data directory, so a failing test can read what it
// left behind.
func (s *Server) Dir() string { return s.dir }

// Lines returns the log so far.
func (s *Server) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.lines...)
}

// Log returns the log so far as one string.
func (s *Server) Log() string { return strings.Join(s.Lines(), "\n") }

// Matching returns the log lines holding substring.
func (s *Server) Matching(substring string) []string {
	var found []string
	for _, line := range s.Lines() {
		if strings.Contains(line, substring) {
			found = append(found, line)
		}
	}

	return found
}

// Stop shuts the server down.
//
// It asks first and kills second: this server saves its world on the way out,
// and a lane that killed it outright would be writing region files half way
// through on every run.
func (s *Server) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()

		return
	}
	s.stopped = true
	s.mu.Unlock()

	if s.cmd.Process == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()

	_ = s.cmd.Process.Signal(os.Interrupt)

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

// read collects the server's output and closes ready at its first sign of life.
func (s *Server) read(out io.Reader, ready chan struct{}) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	announced := false
	for scanner.Scan() {
		line := scanner.Text()

		s.mu.Lock()
		s.lines = append(s.lines, line)
		s.mu.Unlock()

		if !announced && strings.Contains(line, readyLine) {
			announced = true
			close(ready)
		}
	}
}

// waitForPort blocks until the server accepts a connection.
func (s *Server) waitForPort(within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", s.Addr(), time.Second)
		if err == nil {
			_ = conn.Close()

			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("%w: %s never accepted a connection", ErrServer, s.Addr())
}

// freePort picks a port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port
}
