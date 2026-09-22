package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	request, err := decodeRequest(os.Stdin)
	result := response{ProtocolVersion: protocolVersion, Status: "operational-failure"}
	if err != nil {
		result.Failure = boundedFailure(err)
	} else {
		result = newAdapter().run(context.Background(), request)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
