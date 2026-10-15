package supplychain

import (
	"net/http"
	"time"
)

var cargoMetadataRequests = cargoRequestLimit{gate: make(chan struct{}, 1)}

type cargoRequestLimit struct {
	gate chan struct{}
	next time.Time
}

func (limit *cargoRequestLimit) do(client *http.Client, request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	select {
	case limit.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-limit.gate }()
	timer := time.NewTimer(time.Until(limit.next))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit.next = time.Now().Add(time.Second)
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return bounded.Do(request)
}
