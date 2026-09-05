package policy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	policyschema "github.com/riteofstring/code-polishy/schema"
)

func TestShippedSchemaValidatorDoesNotLoadExternalResources(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = writer.Write([]byte("true"))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "permissive.schema.json")
	if err := os.WriteFile(path, []byte("true"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	for _, location := range []string{fileURL, server.URL + "/schema.json"} {
		validator := policyschema.NewValidator(location)
		if err := validator.Validate([]byte("{}")); err == nil || !strings.Contains(err.Error(), "not shipped") {
			t.Fatalf("unshipped resource %s: %v", location, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("schema resolution made an external request")
	}
}
