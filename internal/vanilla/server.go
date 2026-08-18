// Package vanilla starts a real Minecraft server for a test to connect to.
//
// It exists because the only check that survives being wrong in the fifteenth
// decimal place is the game itself: a vanilla server accumulates a client's
// error tick after tick and eventually teleports it back. Everything else this
// repository tests is a statement about what vanilla does; this is where it is
// asked.
//
// The jar is never committed and never downloaded here. It is the artifact
// minecraft-reference prepares and verifies against Mojang's own manifest, and
// when it is absent the caller skips rather than fetches — the same behaviour
// the simulation's oracle has without a JDK, and for the same reason: a check
// that quietly downloaded a hundred megabytes during an ordinary test run would
// be disabled by the next person who ran one.
//
// The server runs in offline mode. That is a limitation of this whole lane and
// it is stated rather than hidden: nothing measured here says anything about
// online-mode behaviour until Microsoft authentication lands.
package vanilla

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
var ErrServer = errors.New("vanilla: server")

// Options configures one server.
type Options struct {
	// Jar is the server to run. When empty, DefaultJar is used.
	Jar string
	// Java is the command that runs it. When empty, "java" from PATH.
	Java string
	// Port is what it listens on. Zero picks a free one, which is what lets two
	// tests run at once.
	Port int
	// Seed and LevelType decide the world. The defaults are a flat world with a
	// fixed seed, because a scenario that walks in a straight line should walk
	// over the same blocks every run.
	Seed      string
	LevelType string
	// Ready is how long to wait for the server to announce itself.
	Ready time.Duration
	// Properties overrides or adds server.properties entries, after the
	// defaults below are written.
	Properties map[string]string
	// Libraries is a directory of jars to put on the classpath, for a version
	// whose server names its dependencies rather than shading them. When it is
	// set the server is started as a class rather than as an executable jar,
	// because that is the only way to add to a jar's own classpath.
	Libraries string
	// MainClass is what to run when Libraries is set.
	MainClass string
}

// DefaultJar is where minecraft-reference leaves a prepared 1.8.9 server, in the
// sibling checkout this repository's own tasks already point at.
const DefaultJar = "../../minecraft-simulation/reference/work/versions/1.8.9/server/original.jar"

// Server is a running server and the log it has written.
type Server struct {
	cmd  *exec.Cmd
	port int
	dir  string
	// console is the server's own command line. It is opened before the process
	// starts because that is the only time a pipe can be attached, which is the
	// whole reason it is kept here rather than asked for when it is needed.
	console io.WriteCloser

	mu    sync.Mutex
	lines []string
	// stopped guards against two shutdowns racing, which a cleanup and a failing
	// test can otherwise do at once.
	stopped bool
}

// Start runs a server and waits for it to accept connections.
//
// It skips the test when the jar or the JVM is absent, and registers its own
// shutdown: a suite that leaked a Minecraft server per run would be turned off
// by whoever noticed, so stopping is not left to the caller.
func Start(t *testing.T, options Options) *Server {
	t.Helper()

	if options.Jar == "" {
		options.Jar = DefaultJar
	}
	jar, err := filepath.Abs(options.Jar)
	if err != nil {
		t.Fatalf("resolve the jar path: %v", err)
	}
	if _, err := os.Stat(jar); err != nil {
		t.Skipf("no prepared server jar at %s; run task server:vanilla once to fetch it",
			options.Jar)
	}

	java := options.Java
	if java == "" {
		java = "java"
	}
	if _, err := exec.LookPath(java); err != nil {
		t.Skipf("%s is not on PATH", java)
	}

	if options.Port == 0 {
		options.Port = freePort(t)
	}
	if options.Ready == 0 {
		options.Ready = 3 * time.Minute
	}

	dir := t.TempDir()
	writeProperties(t, dir, options)
	// The EULA. Writing it is what the human running this has already agreed to
	// by preparing the jar; the file is the server's own gate and it refuses to
	// start without it.
	if err := os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0o600); err != nil {
		t.Fatalf("write eula.txt: %v", err)
	}

	// Small heap and no GUI: this is a server that runs for a minute and serves
	// one client.
	//
	// The two netty properties are what let a server of this age run on a modern
	// JVM. Its bundled netty reaches a direct buffer's address through
	// sun.misc.Unsafe, which a modern runtime no longer permits, and the epoll
	// transport it prefers is the code that does it — so every read fails with
	// "Unable to access address of buffer" and no client ever completes a
	// handshake. Forcing the portable transport and disabling the unsafe path
	// costs some throughput and buys a server that works.
	arguments := []string{
		"-Xms256M", "-Xmx2G",
		"-Dio.netty.transport.noNative=true",
		"-Dio.netty.noUnsafe=true",
	}
	if options.Libraries == "" {
		arguments = append(arguments, "-jar", jar, "nogui")
	} else {
		classpath, err := classpathFor(jar, options.Libraries)
		if err != nil {
			t.Skipf("no prepared libraries at %s: %v", options.Libraries, err)
		}
		main := options.MainClass
		if main == "" {
			main = "net.minecraft.server.Main"
		}
		arguments = append(arguments, "-cp", classpath, main, "nogui")
	}

	cmd := exec.Command(java, arguments...)
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("capture the server's output: %v", err)
	}
	cmd.Stderr = cmd.Stdout

	console, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("open the server's console: %v", err)
	}

	server := &Server{cmd: cmd, port: options.Port, dir: dir, console: console}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the server: %v", err)
	}
	t.Cleanup(func() { server.Stop() })

	ready := make(chan struct{})
	go server.read(stdout, ready)

	select {
	case <-ready:
	case <-time.After(options.Ready):
		t.Fatalf("the server did not become ready in %s\n%s", options.Ready, server.Log())
	}

	return server
}

// Addr returns the address to connect to.
func (s *Server) Addr() string { return "127.0.0.1:" + strconv.Itoa(s.port) }

// Dir returns the server's working directory, so that a failing test can read
// what it left behind.
func (s *Server) Dir() string { return s.dir }

// Lines returns the log so far, one entry per line.
func (s *Server) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.lines...)
}

// Log returns the whole log as text, for a failure message. A gate whose output
// is "one correction" and nothing else cannot be diagnosed by whoever sees it
// next.
func (s *Server) Log() string { return strings.Join(s.Lines(), "\n") }

// Matching returns every log line containing a substring. It is how a test asks
// whether the server complained about a client's movement.
func (s *Server) Matching(substring string) []string {
	var found []string
	for _, line := range s.Lines() {
		if strings.Contains(line, substring) {
			found = append(found, line)
		}
	}

	return found
}

// Stop shuts the server down and waits for the process to exit.
//
// It is registered as a cleanup, so a test that fails, panics, or times out
// still leaves no server behind. A stop that is not answered is escalated:
// a process that ignored a polite shutdown is killed rather than left running.
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

	// The server's own stop command, over its console. A server killed outright
	// can leave a region file half written, and the next run would inherit it.
	_, _ = io.WriteString(s.console, "stop\n")
	_ = s.console.Close()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

// Console runs one command on the server, as the console operator does.
//
// It is how a test makes the server act of its own accord — teleport a player,
// kick one — rather than reacting to something the client sent. Those are the
// behaviours a client-driven test cannot reach at all: a server-initiated
// teleport is not something a client can ask for.
//
// The command carries no leading slash. The console does not take one.
func (s *Server) Console(command string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return fmt.Errorf("%w: it has been stopped", ErrServer)
	}
	if _, err := io.WriteString(s.console, command+"\n"); err != nil {
		return fmt.Errorf("%w: console %q: %w", ErrServer, command, err)
	}

	return nil
}

// Kill ends the server without letting it say goodbye.
//
// It is separate from Stop, and it is not a faster Stop: it is the case a
// clean shutdown cannot produce. A stopped server disconnects its clients with
// a reason, and a client that reads one is a client taking an ordinary path. A
// killed one leaves the connection to fail on its own, which is what a dropped
// route or a crashed process looks like from the other end, and it is the only
// way to test what a client does with work the server will never confirm.
//
// A server killed this way may leave a region file half written. That is
// acceptable in a test whose world is a temporary directory, and it is why this
// is not what Stop does.
func (s *Server) Kill() {
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
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
}

// classpathFor joins a jar with every jar in a directory tree.
//
// A modern server jar names its dependencies rather than shading them, so
// running it needs the libraries the workspace already downloaded beside it.
func classpathFor(jar, libraries string) (string, error) {
	root, err := filepath.Abs(libraries)
	if err != nil {
		return "", err
	}

	entries := []string{jar}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jar") {
			entries = append(entries, path)
		}

		return nil
	}); err != nil {
		return "", err
	}
	if len(entries) == 1 {
		return "", fmt.Errorf("%w: no jars under %s", ErrServer, root)
	}

	return strings.Join(entries, string(os.PathListSeparator)), nil
}

// read collects the server's output and closes ready when it announces itself.
func (s *Server) read(out io.Reader, ready chan struct{}) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	announced := false
	for scanner.Scan() {
		line := scanner.Text()

		s.mu.Lock()
		s.lines = append(s.lines, line)
		s.mu.Unlock()

		// Readiness is read from the log rather than slept for. A sleep long
		// enough for a slow machine is a minute wasted on every fast one, and a
		// sleep short enough for a fast one is a flaky test on a slow one.
		if !announced && strings.Contains(line, `Done (`) {
			announced = true
			close(ready)
		}
	}
}

// writeProperties writes the server.properties a scenario needs.
func writeProperties(t *testing.T, dir string, options Options) {
	t.Helper()

	seed := options.Seed
	if seed == "" {
		seed = "orbit1889"
	}
	level := options.LevelType
	if level == "" {
		level = "FLAT"
	}

	properties := map[string]string{
		// Offline, because Microsoft authentication is postponed and a local
		// check does not need an account. Everything measured here is therefore
		// about offline mode and says nothing about online mode.
		"online-mode": "false",
		"server-port": strconv.Itoa(options.Port),
		"server-ip":   "127.0.0.1",
		"level-seed":  seed,
		"level-type":  level,
		// A flat world with nothing in it: a scenario that walks in a straight
		// line should not meet a mob, and a mob that pushed the player would be
		// a correction nobody could explain.
		"spawn-monsters":      "false",
		"spawn-animals":       "false",
		"spawn-npcs":          "false",
		"generate-structures": "false",
		"allow-nether":        "false",
		"spawn-protection":    "0",
		// The portable transport, not epoll. A server of this age reaches a
		// direct buffer's address through sun.misc.Unsafe inside its native
		// transport, which a modern JVM no longer permits: every read then fails
		// with "Unable to access address of buffer" and no client completes a
		// handshake. This property is the server's own switch for it.
		"use-native-transport": "false",
		"view-distance":        "4",
		"max-players":          "2",
		"motd":                 "conformance",
		// A server that idles the connection out mid-scenario would report a
		// disconnect where the test is measuring movement.
		"player-idle-timeout":  "0",
		"enable-command-block": "false",
		"snooper-enabled":      "false",
	}
	for key, value := range options.Properties {
		properties[key] = value
	}

	var out strings.Builder
	for key, value := range properties {
		fmt.Fprintf(&out, "%s=%s\n", key, value)
	}

	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(out.String()), 0o600); err != nil {
		t.Fatalf("write server.properties: %v", err)
	}
}

// freePort asks the operating system for a port nobody is using.
//
// There is a race between closing this listener and the server binding the same
// port, and it is the standard one: nothing else on the machine is expected to
// take a just-released ephemeral port in the milliseconds between. The
// alternative — a fixed port — makes two runs of this suite collide, which is
// the failure that actually happens.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port
}
