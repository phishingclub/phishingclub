package g

import "github.com/enetx/g/ref"

// Get returns Some(value) if the key exists, otherwise None.
func (e MapSafeEntry[K, V]) Get() Option[V] {
	return e.m.Get(e.key)
}

// Set unconditionally sets the value for the key.
// Returns Some(old_value) if the key was already present, otherwise None.
func (e MapSafeEntry[K, V]) Set(value V) Option[V] {
	return e.m.Set(e.key, value)
}

// Delete atomically retrieves and removes the value for the key from the map.
// Returns Some(value) if it existed, otherwise None.
func (e MapSafeEntry[K, V]) Delete() Option[V] {
	if value, loaded := e.m.data.LoadAndDelete(e.key); loaded {
		return Some(*(value.(*V)))
	}

	return None[V]()
}

// OrSet inserts `value` if the key is vacant.
// Returns the value that is in the map after the operation (either the old or the new one).
func (e MapSafeEntry[K, V]) OrSet(value V) Option[V] {
	actual, loaded := e.m.data.LoadOrStore(e.key, &value)
	if loaded {
		return Some(*(actual.(*V)))
	}

	return None[V]()
}

// OrSetBy inserts the value produced by `fn` if the key is vacant. `fn` is only called if needed.
// Returns the value that is in the map after the operation.
func (e MapSafeEntry[K, V]) OrSetBy(fn func() V) Option[V] {
	if actual, loaded := e.m.data.Load(e.key); loaded {
		return Some(*(actual.(*V)))
	}

	if actual, loaded := e.m.data.LoadOrStore(e.key, ref.Of(fn())); loaded {
		return Some(*(actual.(*V)))
	}

	return None[V]()
}

// OrDefault inserts V's zero value if the key is vacant.
// Returns the value that is in the map after the operation.
func (e MapSafeEntry[K, V]) OrDefault() Option[V] {
	var zero V
	return e.OrSet(zero)
}

// Transform atomically applies `fn` to the existing value if present.
// The function `fn` takes the old value and returns the new value.
// This operation is implemented using a lock-free Compare-And-Swap (CAS) loop.
// Returns Some(updated_value) if successful, or None if the key was missing.
func (e MapSafeEntry[K, V]) Transform(fn func(V) V) Option[V] {
	for {
		avalue, ok := e.m.data.Load(e.key)
		if !ok {
			return None[V]()
		}

		ovalue := avalue.(*V)
		nvalue := fn(*ovalue)

		if e.m.data.CompareAndSwap(e.key, ovalue, &nvalue) {
			return Some(nvalue)
		}
	}
}
