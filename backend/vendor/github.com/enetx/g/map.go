package g

import (
	"fmt"
	"maps"

	"github.com/enetx/g/f"
	"github.com/enetx/iter"
)

// NewMap creates a new Map of the specified size or an empty Map if no size is provided.
func NewMap[K comparable, V any](size ...Int) Map[K, V] {
	return make(Map[K, V], Slice[Int](size).Get(0).UnwrapOrDefault())
}

// Transform applies a transformation function to the Map and returns the result.
func (m Map[K, V]) Transform(fn func(Map[K, V]) Map[K, V]) Map[K, V] { return fn(m) }

// Entry returns an MapEntry object for the given key, providing fine‑grained
// control over insertion and modification of its value.
//
// Example:
//
//	m := g.NewMap[string, int]()
//	// Insert 1 if "foo" is absent, then increment it
//	e := m.Entry("foo")
//	e.OrSet(1)
//	e.Transform(func(v int) int { return v + 1 })
//
// The entire operation requires only a single key lookup and works without
// additional allocations.
func (m Map[K, V]) Entry(key K) MapEntry[K, V] { return MapEntry[K, V]{m, key} }

// Iter returns an iterator (SeqMap[K, V]) for the Map, allowing for sequential iteration
// over its key-value pairs. It is commonly used in combination with higher-order functions,
// such as 'ForEach', to perform operations on each key-value pair of the Map.
//
// Returns:
//
// - SeqMap[K, V], which can be used for sequential iteration over the key-value pairs of the Map.
//
// Example usage:
//
//	myMap := g.Map[string, int]{"one": 1, "two": 2, "three": 3}
//	iterator := myMap.Iter()
//	iterator.ForEach(func(key string, value int) {
//		// Perform some operation on each key-value pair
//		fmt.Printf("%s: %d\n", key, value)
//	})
//
// The 'Iter' method provides a convenient way to traverse the key-value pairs of a Map
// in a functional style, enabling operations like mapping or filtering.
func (m Map[K, V]) Iter() SeqMap[K, V] { return SeqMap[K, V](iter.FromMap(m)) }

// Invert inverts the keys and values of the Map, returning a new Map with values as keys and
// keys as values. Note that the inverted Map will have 'any' as the key type, since not all value
// types are guaranteed to be comparable.
func (m Map[K, V]) Invert() Map[any, K] {
	if m.Empty() {
		return NewMap[any, K]()
	}

	result := make(Map[any, K], len(m))
	for k, v := range m {
		result[v] = k
	}

	return result
}

// Keys returns a slice of the Map's keys.
func (m Map[K, V]) Keys() Slice[K] {
	if m.Empty() {
		return NewSlice[K]()
	}

	keys := make(Slice[K], 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

// Values returns a slice of the Map's values.
func (m Map[K, V]) Values() Slice[V] {
	if m.Empty() {
		return NewSlice[V]()
	}

	values := make(Slice[V], 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}

	return values
}

// Contains checks if the Map contains the specified key.
func (m Map[K, V]) Contains(key K) bool {
	_, ok := m[key]
	return ok
}

// Clone creates a new Map that is a copy of the original Map.
func (m Map[K, V]) Clone() Map[K, V] { return maps.Clone(m) }

// Copy copies the source Map's key-value pairs to the target Map.
func (m Map[K, V]) Copy(src Map[K, V]) { maps.Copy(m, src) }

// Delete removes the specified keys from the Map.
func (m Map[K, V]) Delete(keys ...K) {
	for _, key := range keys {
		delete(m, key)
	}
}

// Std converts the Map to a regular Go map.
func (m Map[K, V]) Std() map[K]V { return m }

// ToMapOrd converts a standard Map to an ordered Map.
func (m Map[K, V]) ToMapOrd() MapOrd[K, V] {
	mo := NewMapOrd[K, V](m.Len())
	for k, v := range m {
		mo.Set(k, v)
	}

	return mo
}

// ToMapSafe converts a standard Map to a thread-safe Map.
func (m Map[K, V]) ToMapSafe() *MapSafe[K, V] {
	ms := NewMapSafe[K, V]()
	for k, v := range m {
		ms.Set(k, v)
	}

	return ms
}

// Eq checks if two Maps are equal.
func (m Map[K, V]) Eq(other Map[K, V]) bool {
	n := len(m)
	if n != len(other) {
		return false
	}

	if n == 0 {
		return true
	}

	var zero V
	comparable := f.IsComparable(zero)

	for k, value := range m {
		ovalue, ok := other[k]
		if !ok || comparable && !f.Eq[any](value)(ovalue) || !comparable && !f.Eqd(value)(ovalue) {
			return false
		}
	}

	return true
}

// String returns a string representation of the Map.
func (m Map[K, V]) String() string {
	if len(m) == 0 {
		return "Map{}"
	}

	var b Builder
	b.WriteString("Map{")

	first := true
	for k, v := range m {
		if !first {
			b.WriteString(", ")
		}

		first = false
		b.WriteString(Format("{}:{}", k, v))
	}

	b.WriteString("}")

	return b.String().Std()
}

// Clear removes all key-value pairs from the Map.
func (m Map[K, V]) Clear() { clear(m) }

// Empty checks if the Map is empty.
func (m Map[K, V]) Empty() bool { return len(m) == 0 }

// Get retrieves the value associated with the given key.
func (m Map[K, V]) Get(k K) Option[V] {
	if v, ok := m[k]; ok {
		return Some(v)
	}

	return None[V]()
}

// Len returns the number of key-value pairs in the Map.
func (m Map[K, V]) Len() Int { return Int(len(m)) }

// Ne checks if two Maps are not equal.
func (m Map[K, V]) Ne(other Map[K, V]) bool { return !m.Eq(other) }

// NotEmpty checks if the Map is not empty.
func (m Map[K, V]) NotEmpty() bool { return !m.Empty() }

// Set sets the value for the key and returns the previous value if it existed.
func (m Map[K, V]) Set(key K, value V) Option[V] {
	prev, ok := m[key]
	m[key] = value
	if ok {
		return Some(prev)
	}

	return None[V]()
}

// Print writes the key-value pairs of the Map to the standard output (console)
// and returns the Map unchanged.
func (m Map[K, V]) Print() Map[K, V] { fmt.Print(m); return m }

// Println writes the key-value pairs of the Map to the standard output (console) with a newline
// and returns the Map unchanged.
func (m Map[K, V]) Println() Map[K, V] { fmt.Println(m); return m }
