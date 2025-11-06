package g

// Get returns Some(value) if the key exists, otherwise None.
func (e MapEntry[K, V]) Get() Option[V] {
	return e.m.Get(e.key)
}

// OrSet inserts value if the key is vacant. Returns Some(existing) or None if newly inserted.
func (e MapEntry[K, V]) OrSet(value V) Option[V] {
	if existing, ok := e.m[e.key]; ok {
		return Some(existing)
	}

	e.m[e.key] = value

	return None[V]()
}

// OrSetBy inserts the value from fn() if the key is vacant. Returns Some(existing) or None.
func (e MapEntry[K, V]) OrSetBy(fn func() V) Option[V] {
	if existing, ok := e.m[e.key]; ok {
		return Some(existing)
	}

	e.m[e.key] = fn()

	return None[V]()
}

// OrDefault inserts the zero value if the key is vacant. Returns Some(existing) or None.
func (e MapEntry[K, V]) OrDefault() Option[V] {
	var zero V
	return e.OrSet(zero)
}

// Transform applies fn to the existing value. Returns Some(updated) or None if key was absent.
func (e MapEntry[K, V]) Transform(fn func(V) V) Option[V] {
	if value, ok := e.m[e.key]; ok {
		value = fn(value)
		e.m[e.key] = value

		return Some(value)
	}

	return None[V]()
}

// Set sets the value and returns Some(previous) if the key existed, or None otherwise.
func (e MapEntry[K, V]) Set(value V) Option[V] {
	old, ok := e.m[e.key]
	e.m[e.key] = value
	if ok {
		return Some(old)
	}

	return None[V]()
}

// Delete removes the key from the map.
// Returns Some(removed_value) if present, None otherwise.
func (e MapEntry[K, V]) Delete() Option[V] {
	if value, ok := e.m[e.key]; ok {
		delete(e.m, e.key)
		return Some(value)
	}

	return None[V]()
}
