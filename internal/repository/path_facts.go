package repository

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type pathFactCache struct {
	excluded       sync.Map
	controlInputs  sync.Map
	generated      sync.Map
	data           sync.Map
	tests          sync.Map
	development    sync.Map
	languages      sync.Map
	executable     sync.Map
	modules        sync.Map
	testOwnerships sync.Map
	ownerModules   sync.Map
	hits           atomic.Int64
	misses         atomic.Int64
	builds         atomic.Int64
}

func (repo Repository) WithPathFactCache() Repository {
	repo.pathFactCache = &pathFactCache{}
	return repo
}

func (repo Repository) WithConfig(config policy.Config) Repository {
	repo.Config = config
	return repo.WithPathFactCache()
}

func (repo Repository) PathFactCacheStats() (int64, int64, int64) {
	if repo.pathFactCache == nil {
		return 0, 0, 0
	}
	return repo.pathFactCache.hits.Load(), repo.pathFactCache.misses.Load(), repo.pathFactCache.builds.Load()
}

func cachedPathFact[T any](cache *pathFactCache, values *sync.Map, path string, clone func(T) T, compute func() T) T {
	if cached, present := values.Load(path); present {
		cache.hits.Add(1)
		return clone(cached.(T))
	}
	cache.misses.Add(1)
	computed := compute()
	actual, loaded := values.LoadOrStore(path, computed)
	if loaded {
		cache.hits.Add(1)
	} else {
		cache.builds.Add(1)
	}
	return clone(actual.(T))
}

func cloneBool(value bool) bool {
	return value
}

func cloneStrings(values []string) []string {
	return slices.Clone(values)
}

func cloneTestOwnerships(values []policy.TestOwnership) []policy.TestOwnership {
	cloned := slices.Clone(values)
	for index := range cloned {
		cloned[index].Paths = slices.Clone(cloned[index].Paths)
	}
	return cloned
}
