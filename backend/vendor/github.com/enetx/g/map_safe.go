package g

import (
	"fmt"

	"github.com/enetx/g/f"
)

// NewMapSafe creates a new instance of MapSafe.
func NewMapSafe[K comparable, V any]() *MapSafe[K, V] { return &MapSafe[K, V]{} }

// Iter provides a thread-safe iterator over the MapSafe's key-value pairs.
func (ms *MapSafe[K, V]) Iter() SeqMap[K, V] {
	return func(yield func(K, V) bool) {
		ms.data.Range(func(key, value any) bool {
			return yield(key.(K), *(value.(*V)))
		})
	}
}

// Entry returns a MapSafeEntry for a given key, allowing for more complex atomic operations.
func (ms *MapSafe[K, V]) Entry(key K) MapSafeEntry[K, V] {
	return MapSafeEntry[K, V]{m: ms, key: key}
}

// Keys returns a slice of the MapSafe's keys.
func (ms *MapSafe[K, V]) Keys() Slice[K] {
	var keys Slice[K]

	ms.data.Range(func(key, _ any) bool {
		keys = append(keys, key.(K))
		return true
	})

	return keys
}

// Values returns a slice of the MapSafe's values.
func (ms *MapSafe[K, V]) Values() Slice[V] {
	var values Slice[V]

	ms.data.Range(func(_, value any) bool {
		values = append(values, *(value.(*V)))
		return true
	})

	return values
}

// Invert inverts keys and values. The new map will also follow the pointer-storage rule.
func (ms *MapSafe[K, V]) Invert() *MapSafe[any, K] {
	res := NewMapSafe[any, K]()

	ms.data.Range(func(key, value any) bool {
		k := key.(K)
		res.data.Store(*(value.(*V)), &k)
		return true
	})

	return res
}

// Contains checks if the MapSafe contains the specified key.
func (ms *MapSafe[K, V]) Contains(key K) bool {
	_, ok := ms.data.Load(key)
	return ok
}

// Clone creates a deep copy of the MapSafe.
func (ms *MapSafe[K, V]) Clone() *MapSafe[K, V] {
	res := NewMapSafe[K, V]()

	ms.data.Range(func(key, value any) bool {
		v := *(value.(*V))
		res.data.Store(key, &v)
		return true
	})

	return res
}

// Copy performs a deep copy of the source MapSafe's pairs into the current map.
func (ms *MapSafe[K, V]) Copy(src *MapSafe[K, V]) {
	src.data.Range(func(key, value any) bool {
		v := *(value.(*V))
		ms.data.Store(key, &v)
		return true
	})
}

// Delete removes the specified keys from the MapSafe.
func (ms *MapSafe[K, V]) Delete(keys ...K) {
	for _, k := range keys {
		ms.data.Delete(k)
	}
}

// Eq checks if two MapSafes are equal by deep-comparing their values.
func (ms *MapSafe[K, V]) Eq(other *MapSafe[K, V]) bool {
	n := ms.Len()
	if n != other.Len() {
		return false
	}

	if n == 0 {
		return true
	}

	var zero V
	comparable := f.IsComparable(zero)

	equal := true

	ms.data.Range(func(key, value any) bool {
		ovalue, ok := other.data.Load(key)
		if !ok {
			equal = false
			return false
		}

		v1 := *(value.(*V))
		v2 := *(ovalue.(*V))

		if comparable && !f.Eq[any](v1)(v2) || !comparable && !f.Eqd(v1)(v2) {
			equal = false
			return false
		}

		return true
	})

	return equal
}

// Get retrieves the value associated with the given key.
func (ms *MapSafe[K, V]) Get(key K) Option[V] {
	if value, ok := ms.data.Load(key); ok {
		return Some(*(value.(*V)))
	}

	return None[V]()
}

// Set stores the value for the given key, returning the previous value if it existed.
func (ms *MapSafe[K, V]) Set(key K, value V) Option[V] {
	if previous, loaded := ms.data.Swap(key, &value); loaded {
		return Some(*(previous.(*V)))
	}

	return None[V]()
}

// Len returns the number of key-value pairs in the MapSafe.
func (ms *MapSafe[K, V]) Len() int {
	count := 0

	ms.data.Range(func(_, _ any) bool {
		count++
		return true
	})

	return count
}

// Ne checks if two MapSafes are not equal.
func (ms *MapSafe[K, V]) Ne(other *MapSafe[K, V]) bool { return !ms.Eq(other) }

// NotEmpty checks if the MapSafe is not empty.
func (ms *MapSafe[K, V]) NotEmpty() bool { return !ms.Empty() }

// Clear removes all key-value pairs from the MapSafe.
func (ms *MapSafe[K, V]) Clear() { ms.data.Clear() }

// Empty checks if the MapSafe is empty.
func (ms *MapSafe[K, V]) Empty() bool {
	empty := true

	ms.data.Range(func(_, _ any) bool {
		empty = false
		return false
	})

	return empty
}

// String returns a string representation of the MapSafe.
func (ms *MapSafe[K, V]) String() string {
	var b Builder
	b.WriteString("MapSafe{")

	first := true

	ms.data.Range(func(key, value any) bool {
		if !first {
			b.WriteString(", ")
		}

		first = false

		if vptr, ok := value.(*V); ok && vptr != nil {
			b.WriteString(Format("{}:{}", key, *vptr))
		} else {
			b.WriteString(Format("{}:<invalid>", key))
		}

		return true
	})

	b.WriteString("}")

	return b.String().Std()
}

// Print writes the MapSafe to standard output.
func (ms *MapSafe[K, V]) Print() *MapSafe[K, V] { fmt.Print(ms); return ms }

// Println writes the MapSafe to standard output with a newline.
func (ms *MapSafe[K, V]) Println() *MapSafe[K, V] { fmt.Println(ms); return ms }
