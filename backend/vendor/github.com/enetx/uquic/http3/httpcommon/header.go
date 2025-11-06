package httpcommon

import (
	"sort"
	"strings"
	"sync"

	"github.com/enetx/http"
)

const (
	// HeaderOrderKey is a magic key for ResponseWriter.Header map keys
	// that, if present, defines a header order that will be used to
	// write the headers onto wire. The order of the list defined how the headers
	// will be sorted. A defined key goes before an undefined key.
	//
	// This is the only way to specify some order, because maps don't
	// have a a stable iteration order. If no order is given, headers will
	// be sorted lexicographically.
	//
	// According to RFC-2616 it is good practice to send general-header fields
	// first, followed by request-header or response-header fields and ending
	// with entity-header fields.
	HeaderOrderKey = "Header-Order:"

	// PHeaderOrderKey is a magic key for setting http3 pseudo header order.
	// If the header is nil it will use regular GoLang header order.
	// Valid fields are :authority, :method, :path, :scheme, :protocol
	PHeaderOrderKey = "PHeader-Order:"
)

// HeaderKeyValues represents a key-value pair for headers
type HeaderKeyValues struct {
	Key    string
	Values []string
}

// A HeaderSorter implements sort.Interface by sorting a []HeaderKeyValues
// by key. It's used as a pointer, so it can fit in a sort.Interface
// interface value without allocation.
type HeaderSorter struct {
	kvs   []HeaderKeyValues
	order map[string]int
}

func (s *HeaderSorter) Len() int      { return len(s.kvs) }
func (s *HeaderSorter) Swap(i, j int) { s.kvs[i], s.kvs[j] = s.kvs[j], s.kvs[i] }
func (s *HeaderSorter) Less(i, j int) bool {
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

var headerSorterPool = sync.Pool{
	New: func() any { return new(HeaderSorter) },
}

var lock = sync.RWMutex{}

// SortedKeyValues returns h's keys sorted in the returned kvs
// slice. The HeaderSorter used to sort is also returned, for possible
// return to headerSorterPool.
func SortedKeyValues(h http.Header, exclude map[string]bool) (kvs []HeaderKeyValues, hs *HeaderSorter) {
	hs = headerSorterPool.Get().(*HeaderSorter)
	if cap(hs.kvs) < len(h) {
		hs.kvs = make([]HeaderKeyValues, 0, len(h))
	}

	kvs = hs.kvs[:0]
	for k, vv := range h {
		lock.RLock()
		if !exclude[k] {
			kvs = append(kvs, HeaderKeyValues{k, vv})
		}
		lock.RUnlock()
	}

	hs.kvs = kvs
	sort.Sort(hs)

	return kvs, hs
}

// SortedKeyValuesBy returns headers sorted by specified order
func SortedKeyValuesBy(
	h http.Header,
	order map[string]int,
	exclude map[string]bool,
) (kvs []HeaderKeyValues, hs *HeaderSorter) {
	hs = headerSorterPool.Get().(*HeaderSorter)
	if cap(hs.kvs) < len(h) {
		hs.kvs = make([]HeaderKeyValues, 0, len(h))
	}

	kvs = hs.kvs[:0]
	for k, vv := range h {
		lock.RLock()
		if !exclude[k] {
			kvs = append(kvs, HeaderKeyValues{k, vv})
		}
		lock.RUnlock()
	}

	hs.kvs = kvs
	hs.order = order
	sort.Sort(hs)

	return kvs, hs
}

// ReturnSorter returns a header sorter to the pool
func ReturnSorter(hs *HeaderSorter) {
	headerSorterPool.Put(hs)
}
