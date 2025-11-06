package httpcommon

import (
	"maps"
	"sort"
	"strings"
	"sync"
)

type Header map[string][]string

var lock = sync.RWMutex{}

const (
	HeaderOrderKey  = "Header-Order:"
	PHeaderOrderKey = "PHeader-Order:"
)

func (h Header) Clone() Header { return maps.Clone(h) }

type headerKeyValues struct {
	Key    string
	Values []string
}

type headerSorter struct {
	kvs   []headerKeyValues
	order map[string]int
}

var headerSorterPool = sync.Pool{
	New: func() any { return new(headerSorter) },
}

func (s *headerSorter) Len() int      { return len(s.kvs) }
func (s *headerSorter) Swap(i, j int) { s.kvs[i], s.kvs[j] = s.kvs[j], s.kvs[i] }
func (s *headerSorter) Less(i, j int) bool {
	// If the order isn't defined, sort lexicographically.
	if s.order == nil {
		return s.kvs[i].Key < s.kvs[j].Key
	}

	idxi, iok := s.order[strings.ToLower(s.kvs[i].Key)]
	idxj, jok := s.order[strings.ToLower(s.kvs[j].Key)]
	if !iok && !jok {
		return s.kvs[i].Key < s.kvs[j].Key
	} else if !iok && jok {
		return false
	} else if iok && !jok {
		return true
	}

	return idxi < idxj
}

func (h Header) SortedKeyValues(exclude map[string]bool) (kvs []headerKeyValues, hs *headerSorter) {
	hs = headerSorterPool.Get().(*headerSorter)
	if cap(hs.kvs) < len(h) {
		hs.kvs = make([]headerKeyValues, 0, len(h))
	}

	kvs = hs.kvs[:0]
	for k, vv := range h {
		lock.RLock()
		if !exclude[k] {
			kvs = append(kvs, headerKeyValues{k, vv})
		}
		lock.RUnlock()
	}

	hs.kvs = kvs
	sort.Sort(hs)

	return kvs, hs
}

func (h Header) SortedKeyValuesBy(
	order map[string]int,
	exclude map[string]bool,
) (kvs []headerKeyValues, hs *headerSorter) {
	hs = headerSorterPool.Get().(*headerSorter)
	if cap(hs.kvs) < len(h) {
		hs.kvs = make([]headerKeyValues, 0, len(h))
	}

	kvs = hs.kvs[:0]
	for k, vv := range h {
		lock.RLock()
		if !exclude[k] {
			kvs = append(kvs, headerKeyValues{k, vv})
		}
		lock.RUnlock()
	}

	hs.kvs = kvs
	hs.order = order
	sort.Sort(hs)

	return kvs, hs
}
