//go:build vanilla

// Where the vanilla lane's jars come from, and the record of which build ran.
//
// M10 wanted a pinned artifact manifest so a conformance result names the
// build it tested. minecraft-reference already is that pin: it downloads by
// URL, verifies SHA-1 and SHA-256 against Mojang's own manifest before a file
// reaches its path, and writes the record into the prepared workspace's
// manifest.lock.json. What was missing was on this side — the lane hardcoded
// a relative path into a sibling repository's gitignored workspace, so it
// passed on a machine where a different repository happened to be prepared,
// skipped everywhere else, and nothing said which build ran.
//
// Resolution order: an explicit MCREFERENCE_WORKSPACE, then this repository's
// own reference/work. Never a relative path into a sibling — a test that
// reads another repository's ignored directory passes or fails for reasons
// its own repository cannot see. A missing workspace skips, deliberately:
// the lane is behind the vanilla build tag and out of verify, and a lane
// that failed without a jar would make verify depend on a download.
package client_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/internal/vanilla"
)

// vanillaWorkspace resolves the prepared reference workspace.
func vanillaWorkspace() string {
	if explicit := os.Getenv("MCREFERENCE_WORKSPACE"); explicit != "" {
		return explicit
	}

	return filepath.Join("..", "reference", "work")
}

// preparedBuild is one version's prepared server, read from the workspace's
// own record rather than trusted from a path.
type preparedBuild struct {
	Version string
	// Jar is what runs: the extracted executable where the version bundles
	// one, the original otherwise.
	Jar string
	// Libraries is the classpath directory, for a version whose server names
	// its dependencies rather than shading them. Empty for one that shades.
	Libraries string
	// Digest identifies the server artifact, algorithm-prefixed: the
	// sha256 the workspace's manifest.lock.json recorded when the server
	// side was locked, or Mojang's own sha1 for it from the cached version
	// metadata otherwise. Either way it is the identity of the build every
	// result from this lane is a claim about, recorded at download time
	// rather than recomputed from the file.
	Digest string
}

// prepared reads one version's build out of a workspace.
func prepared(workspace, version string) (preparedBuild, error) {
	root := filepath.Join(workspace, "versions", version)
	content, err := os.ReadFile(filepath.Join(root, "manifest.lock.json"))
	if err != nil {
		return preparedBuild{}, fmt.Errorf("no manifest for %s: %w", version, err)
	}

	var manifest struct {
		Artifacts []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return preparedBuild{}, fmt.Errorf("read the %s manifest: %w", version, err)
	}

	build := preparedBuild{Version: version}
	var metadata string
	for _, artifact := range manifest.Artifacts {
		path := filepath.ToSlash(artifact.Path)
		switch {
		case strings.HasSuffix(path, "/server/original.jar"):
			build.Digest = "sha256:" + artifact.SHA256
		case strings.HasSuffix(path, "/cache/metadata/"+version+".json"):
			metadata = filepath.Join(workspace, "cache", "metadata", version+".json")
		}
	}
	if build.Digest == "" && metadata != "" {
		// The lock records what this workspace downloaded, and a workspace
		// prepared client-side first has no server entry. Mojang's own
		// version metadata — itself a locked artifact — pins the server jar,
		// so its digest is the fallback record.
		digest, err := mojangServerDigest(metadata)
		if err != nil {
			return preparedBuild{}, fmt.Errorf("the %s metadata names no server jar: %w", version, err)
		}
		build.Digest = digest
	}
	if build.Digest == "" {
		return preparedBuild{}, fmt.Errorf("the %s manifest records no server jar digest", version)
	}

	build.Jar = filepath.Join(root, "server", "original.jar")
	if executable := filepath.Join(root, "server", "executable.jar"); exists(executable) {
		// A bundler version, extracted: it runs as a class over the library
		// directory. The libraries only matter alongside the extracted jar —
		// a shaded server like 1.8.9's carries everything and its workspace's
		// libraries directory is the client's.
		build.Jar = executable
		if libraries := filepath.Join(root, "libraries"); exists(libraries) {
			build.Libraries = libraries
		}
	}
	if !exists(build.Jar) {
		return preparedBuild{}, fmt.Errorf("the %s manifest is present and the jar is not", version)
	}

	return build, nil
}

// mojangServerDigest reads the server jar's pin out of Mojang's version
// metadata.
func mojangServerDigest(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var metadata struct {
		Downloads struct {
			Server struct {
				SHA1 string `json:"sha1"`
			} `json:"server"`
		} `json:"downloads"`
	}
	if err := json.Unmarshal(content, &metadata); err != nil {
		return "", err
	}
	if metadata.Downloads.Server.SHA1 == "" {
		return "", fmt.Errorf("no downloads.server.sha1 in %s", path)
	}

	return "sha1:" + metadata.Downloads.Server.SHA1, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// laneBuild resolves one version's build for a lane, logging the digest so
// the run names the build it ran against, and skipping — with the command
// that fixes it — when none is prepared.
func laneBuild(t *testing.T, version string) preparedBuild {
	t.Helper()

	workspace := vanillaWorkspace()
	build, err := prepared(workspace, version)
	if err != nil {
		t.Skipf("no prepared %s reference workspace under %s: %v; "+
			"run `task server:vanilla VERSION=%s` or set MCREFERENCE_WORKSPACE",
			version, workspace, err, version)
	}
	t.Logf("%s server jar %s", version, build.Digest)

	return build
}

// startOptions renders a build into the server harness's options.
func (b preparedBuild) startOptions() vanilla.Options {
	options := vanilla.Options{Jar: b.Jar, Libraries: b.Libraries}
	if b.Libraries != "" {
		options.LevelType = "minecraft:flat"
	}

	return options
}

func TestVanillaTheLaneNamesTheBuildItRan(t *testing.T) {
	// A conformance result that does not say which jar produced it is a story
	// about somebody's laptop. This reads the prepared workspace's own record
	// rather than trusting a path, for both versions, and the per-lane
	// resolution logs the same digests on every run.
	for _, version := range []string{"1.8.9", "26.1.2"} {
		build := laneBuild(t, version)
		if build.Digest == "" {
			t.Fatalf("the workspace records no digest for %s", version)
		}
	}
}
