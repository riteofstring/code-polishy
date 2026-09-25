package supplychain

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCargoRegistryRequestsRemainSpacedAfterCancellation(t *testing.T) {
	t.Parallel()
	limit := cargoRequestLimit{gate: make(chan struct{}, 1)}
	transport := &cargoRequestTransport{}
	client := &http.Client{Transport: transport}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://crates.io/api/v1/crates/example/versions", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := limit.do(client, request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := limit.do(client, request.Clone(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request = %v", err)
	}
	response, err = limit.do(client, request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(transport.starts) != 2 || transport.starts[1].Sub(transport.starts[0]) < 990*time.Millisecond {
		t.Fatalf("crate requests exceeded one per second or cancellation sent a request: %v", transport.starts)
	}
}

func TestCargoRegistryCancellationReleasesQueuedCallers(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"queue", "rate wait"} {
		t.Run(phase, func(t *testing.T) {
			limit := cargoRequestLimit{gate: make(chan struct{}, 1), next: time.Now().Add(time.Hour)}
			if phase == "queue" {
				limit.gate <- struct{}{}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://crates.io/api/v1/crates/example/versions", nil)
			if err != nil {
				t.Fatal(err)
			}
			transport := &cargoRequestTransport{}
			if _, err := limit.do(&http.Client{Transport: transport}, request); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blocked caller ignored cancellation: %v", err)
			}
			if len(transport.starts) != 0 {
				t.Fatal("canceled queued caller reached the registry")
			}
		})
	}
}

func TestCargoRegistryDoesNotFollowMetadataRedirects(t *testing.T) {
	t.Parallel()
	limit := cargoRequestLimit{gate: make(chan struct{}, 1)}
	transport := &cargoRequestTransport{redirect: true}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://crates.io/api/v1/crates/example/versions", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := limit.do(&http.Client{Transport: transport}, request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound || len(transport.starts) != 1 {
		t.Fatalf("foreign metadata redirect was followed: %d, %v", response.StatusCode, transport.starts)
	}
}

type cargoRequestTransport struct {
	starts   []time.Time
	redirect bool
}

func (transport *cargoRequestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.starts = append(transport.starts, time.Now())
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}
	if transport.redirect {
		response.StatusCode = http.StatusFound
		response.Header.Set("Location", "https://foreign.example.test/metadata")
	}
	return response, nil
}
