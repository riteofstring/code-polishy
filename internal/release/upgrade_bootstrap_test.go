package release

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpgradeBootstrapDiagnosticsRemainBoundedWhenCopied(t *testing.T) {
	t.Parallel()
	output := &bootstrapOutput{}
	written, err := io.Copy(output, io.LimitReader(strings.NewReader(strings.Repeat("x", 128<<10)), 128<<10))
	if err != nil || written != 128<<10 || len(output.String()) != 64<<10 {
		t.Fatalf("bootstrap diagnostics escaped their bound: written=%d retained=%d error=%v", written, len(output.String()), err)
	}
}

func TestUpgradeBootstrapsAnAuthenticatedFutureManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX incoming-bootstrap fixture")
	}
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("f", 64)
	bootstrap := "#!/bin/sh\nset -eu\nwhile [ $# -gt 0 ]; do\ncase \"$1\" in\n--source) source=$2; shift;;\n--prefix) prefix=$2; shift;;\nesac\nshift\ndone\nmkdir -p \"$prefix/releases/9.9.9-" + digest + "\"\nunzip -oq \"$source\" -d \"$prefix/releases/9.9.9-" + digest + "\"\n"
	root, manifest := installedRelease(t, map[string]string{BinaryPath: bootstrap, LauncherBinaryPath: "future launcher"}, nil)
	manifest.CodePolishyVersion, manifest.ReleaseDigest = "9.9.9", digest
	manifest.ManifestVersion = ManifestVersion + 1
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["futureVerification"] = "owned by the incoming bootstrap"
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
	archive := zipDirectory(t, root)
	archiveData, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	index := publicationIndexFixture(manifest, digestBytes(archiveData), int64(len(archiveData)))
	for i := range index.Artifacts {
		index.Artifacts[i].Manifest.SHA256 = digestBytes(data)
	}
	indexData, err := renderJSON(index)
	if err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []bool{false, true} {
		prefix := t.TempDir()
		served := archiveData
		if corrupt {
			served = []byte("unauthenticated archive")
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/index.json" {
				_, _ = response.Write(indexData)
			} else {
				_, _ = response.Write(served)
			}
		}))
		defer server.Close()
		_, err := installIndexedRelease(context.Background(), server.Client(), server.URL+"/index.json", digestBytes(indexData), prefix)
		var handoff *UpgradeHandoff
		if corrupt {
			if err == nil || errors.As(err, &handoff) {
				t.Fatalf("unauthenticated archive ran its bootstrap: %v", err)
			}
			continue
		}
		if !errors.As(err, &handoff) {
			t.Fatalf("future manifest did not reach its own bootstrap: %v", err)
		}
		installed, readErr := os.ReadFile(filepath.Join(handoff.Root, ManifestFilename))
		if readErr != nil || string(installed) != string(data) {
			t.Fatalf("incoming bootstrap did not install its manifest: %v", readErr)
		}
	}
}
