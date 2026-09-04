package pythonfacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maximumProjectSources    = 65536
	maximumProjectSourceSize = 512 * 1024 * 1024
	maximumProjectFactSize   = 256 * 1024 * 1024
	maximumProjectPartitions = 256
	maximumProjectDuration   = 15 * time.Minute
	maximumSourceSize        = 2 * 1024 * 1024
	maximumSourcePathSize    = 4096
)

type SourceProject struct {
	Sources    []Source          `json:"sources"`
	Partitions []SourcePartition `json:"partitions"`
	Identity   string            `json:"identity"`
}

type SourcePartition struct {
	Index          int    `json:"index"`
	Count          int    `json:"count"`
	FirstPath      string `json:"firstPath"`
	LastPath       string `json:"lastPath"`
	RequestSHA256  string `json:"requestSha256"`
	ResponseSHA256 string `json:"responseSha256"`
}

type sourceAnalyzer func(context.Context, string, Request) (Response, error)

type analyzedSourcePartition struct {
	sources []Source
	record  SourcePartition
	bytes   int
}

type sourcePartitionBuilder struct {
	limit       int
	baseSize    int
	currentSize int
	current     []Input
	partitions  [][]Input
}

func AnalyzeProjectSources(ctx context.Context, python string, sources []Input) (SourceProject, error) {
	bounded, cancel := context.WithTimeout(ctx, maximumProjectDuration)
	defer cancel()
	return analyzeProjectSources(bounded, python, sources, maximumRequestSize, analyze)
}

func analyzeProjectSources(ctx context.Context, python string, sources []Input, requestLimit int, analyzer sourceAnalyzer) (SourceProject, error) {
	ordered, err := normalizedProjectSources(sources)
	if err != nil {
		return SourceProject{}, err
	}
	partitions, err := partitionProjectSources(ordered, requestLimit)
	if err != nil {
		return SourceProject{}, err
	}
	project, err := analyzeSourcePartitions(ctx, python, partitions, requestLimit, analyzer, len(ordered))
	if err != nil {
		return SourceProject{}, err
	}
	if err := validateProjectSourceCoverage(ordered, project.Sources); err != nil {
		return SourceProject{}, err
	}
	identity, err := sourceProjectIdentity(project.Sources)
	if err != nil {
		return SourceProject{}, err
	}
	project.Identity = identity
	return project, nil
}

func analyzeSourcePartitions(
	ctx context.Context,
	python string,
	partitions [][]Input,
	requestLimit int,
	analyzer sourceAnalyzer,
	sourceCount int,
) (SourceProject, error) {
	project := SourceProject{Sources: make([]Source, 0, sourceCount), Partitions: make([]SourcePartition, 0, len(partitions))}
	factBytes := 0
	for index, partition := range partitions {
		analyzed, err := analyzeSourcePartition(ctx, python, partition, index, len(partitions), requestLimit, analyzer)
		if err != nil {
			return SourceProject{}, err
		}
		if analyzed.bytes > maximumProjectFactSize-factBytes {
			return SourceProject{}, errors.New("python-facts project exceeds the compact fact byte limit")
		}
		factBytes += analyzed.bytes
		project.Sources = append(project.Sources, analyzed.sources...)
		project.Partitions = append(project.Partitions, analyzed.record)
	}
	return project, nil
}

func analyzeSourcePartition(
	ctx context.Context,
	python string,
	partition []Input,
	index, total, requestLimit int,
	analyzer sourceAnalyzer,
) (analyzedSourcePartition, error) {
	if err := ctx.Err(); err != nil {
		return analyzedSourcePartition{}, errors.New("python-facts project exceeded its time limit")
	}
	request := normalizedRequest(Request{Sources: partition})
	requestData, err := encodeRequestWithLimit(request, requestLimit)
	if err != nil {
		return analyzedSourcePartition{}, fmt.Errorf("python-facts project partition %d is invalid: %w", index+1, err)
	}
	response, err := analyzer(ctx, python, request)
	if err != nil {
		return analyzedSourcePartition{}, fmt.Errorf("python-facts project partition %d of %d failed: %w", index+1, total, err)
	}
	if err := validateResponse(response, request); err != nil {
		return analyzedSourcePartition{}, fmt.Errorf("python-facts project partition %d of %d is invalid: %w", index+1, total, err)
	}
	if err := validateProjectSourceCoverage(partition, response.Sources); err != nil {
		return analyzedSourcePartition{}, fmt.Errorf("python-facts project partition %d of %d is invalid: %w", index+1, total, err)
	}
	responseData, err := json.Marshal(response)
	if err != nil {
		return analyzedSourcePartition{}, fmt.Errorf("encode python-facts project partition %d response: %w", index+1, err)
	}
	if len(responseData) > maximumResponseSize {
		return analyzedSourcePartition{}, fmt.Errorf("python-facts project partition %d response exceeds the byte limit", index+1)
	}
	return analyzedSourcePartition{
		sources: response.Sources, record: sourcePartitionIdentity(index, partition, requestData, responseData), bytes: len(responseData),
	}, nil
}

func normalizedProjectSources(sources []Input) ([]Input, error) {
	if len(sources) > maximumProjectSources {
		return nil, errors.New("python-facts project exceeds the source item limit")
	}
	ordered := append([]Input{}, sources...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Path < ordered[right].Path })
	total := 0
	previous := ""
	for _, source := range ordered {
		if !validProjectSourcePath(source.Path) || !utf8.ValidString(source.Source) {
			return nil, fmt.Errorf("python-facts project contains an invalid source input: %q", source.Path)
		}
		if source.Path == previous {
			return nil, fmt.Errorf("python-facts project contains duplicate source path %q", source.Path)
		}
		if len(source.Source) > maximumSourceSize {
			return nil, fmt.Errorf("python-facts project source exceeds the per-source byte limit: %s", source.Path)
		}
		if len(source.Source) > maximumProjectSourceSize-total {
			return nil, errors.New("python-facts project exceeds the aggregate source byte limit")
		}
		total += len(source.Source)
		previous = source.Path
	}
	return ordered, nil
}

func validProjectSourcePath(value string) bool {
	return value != "" && len(value) <= maximumSourcePathSize && utf8.ValidString(value) && !strings.Contains(value, "\\") && !strings.HasPrefix(value, "/") &&
		value != "." && value != ".." && !strings.HasPrefix(value, "../") && path.Clean(value) == value
}

func partitionProjectSources(sources []Input, requestLimit int) ([][]Input, error) {
	builder, err := newSourcePartitionBuilder(requestLimit)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if err := builder.add(source); err != nil {
			return nil, err
		}
	}
	return builder.result()
}

func newSourcePartitionBuilder(requestLimit int) (*sourcePartitionBuilder, error) {
	if requestLimit <= 0 || requestLimit > maximumRequestSize {
		return nil, errors.New("python-facts project has an invalid partition byte limit")
	}
	emptyData, err := json.Marshal(normalizedRequest(Request{}))
	if err != nil {
		return nil, err
	}
	baseSize := len(emptyData)
	if baseSize > requestLimit {
		return nil, errors.New("python-facts project partition byte limit cannot hold the protocol envelope")
	}
	return &sourcePartitionBuilder{limit: requestLimit, baseSize: baseSize, currentSize: baseSize}, nil
}

func (builder *sourcePartitionBuilder) add(source Input) error {
	encoded, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("encode python-facts source %s: %w", source.Path, err)
	}
	if builder.needsFlush(len(encoded)) {
		if len(builder.current) == 0 {
			return fmt.Errorf("python-facts source cannot fit one bounded partition: %s", source.Path)
		}
		builder.flush()
	}
	increment := builder.increment(len(encoded))
	if builder.currentSize+increment > builder.limit {
		return fmt.Errorf("python-facts source cannot fit one bounded partition: %s", source.Path)
	}
	builder.current = append(builder.current, source)
	builder.currentSize += increment
	return nil
}

func (builder *sourcePartitionBuilder) needsFlush(encodedSize int) bool {
	return len(builder.current) == maximumItems || builder.currentSize+builder.increment(encodedSize) > builder.limit
}

func (builder *sourcePartitionBuilder) increment(encodedSize int) int {
	if len(builder.current) == 0 {
		return encodedSize
	}
	return encodedSize + 1
}

func (builder *sourcePartitionBuilder) flush() {
	builder.partitions = append(builder.partitions, builder.current)
	builder.current = []Input{}
	builder.currentSize = builder.baseSize
}

func (builder *sourcePartitionBuilder) result() ([][]Input, error) {
	if len(builder.current) > 0 {
		builder.flush()
	}
	if len(builder.partitions) > maximumProjectPartitions {
		return nil, errors.New("python-facts project exceeds the partition count limit")
	}
	return builder.partitions, nil
}

func encodeRequestWithLimit(request Request, limit int) ([]byte, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("python-facts request exceeds the partition byte limit")
	}
	return data, nil
}

func sourcePartitionIdentity(index int, sources []Input, requestData, responseData []byte) SourcePartition {
	requestDigest := sha256.Sum256(requestData)
	responseDigest := sha256.Sum256(responseData)
	return SourcePartition{
		Index: index + 1, Count: len(sources), FirstPath: sources[0].Path, LastPath: sources[len(sources)-1].Path,
		RequestSHA256: hex.EncodeToString(requestDigest[:]), ResponseSHA256: hex.EncodeToString(responseDigest[:]),
	}
}

func validateProjectSourceCoverage(requested []Input, actual []Source) error {
	if len(requested) != len(actual) {
		return errors.New("python-facts project response has an invalid source count")
	}
	for index := range requested {
		if requested[index].Path != actual[index].Path {
			return errors.New("python-facts project response omitted, duplicated, or reordered a source")
		}
		digest := sha256.Sum256([]byte(requested[index].Source))
		if actual[index].SHA256 != hex.EncodeToString(digest[:]) {
			return fmt.Errorf("python-facts project response has a stale source digest for %s", requested[index].Path)
		}
	}
	return nil
}

func sourceProjectIdentity(sources []Source) (string, error) {
	data, err := json.Marshal(sources)
	if err != nil {
		return "", fmt.Errorf("encode python-facts project identity: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
