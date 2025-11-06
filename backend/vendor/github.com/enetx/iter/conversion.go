package iter

import (
	"context"
	"iter"
)

// Pull converts a push-style iterator (Seq) to a pull-style iterator.
// Returns a next function that yields the next value and a boolean indicating if valid,
// and a stop function that should be called to release resources.
//
// Example:
//
//	next, stop := iter.Pull(iter.FromSlice([]int{1, 2, 3}))
//	defer stop()
//	for {
//	  v, ok := next()
//	  if !ok { break }
//	  fmt.Println(v)
//	}
func Pull[T any](s Seq[T]) (next func() (T, bool), stop func()) {
	return iter.Pull(iter.Seq[T](s))
}

// Pull2 converts a push-style iterator (Seq2) to a pull-style iterator.
func Pull2[K, V any](s Seq2[K, V]) (next func() (K, V, bool), stop func()) {
	return iter.Pull2(iter.Seq2[K, V](s))
}

// Context wraps a sequence with context cancellation.
// If the context is cancelled, iteration stops early.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
//	defer cancel()
//	s := iter.Context(iter.FromSlice([]int{1, 2, 3}), ctx)
func Context[T any](s Seq[T], ctx context.Context) Seq[T] {
	return func(yield func(T) bool) {
		if err := ctx.Err(); err != nil {
			return
		}
		s(func(v T) bool {
			select {
			case <-ctx.Done():
				return false
			default:
				return yield(v)
			}
		})
	}
}

// Context2 wraps a key-value sequence with context cancellation.
// If the context is cancelled, iteration stops early.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
//	defer cancel()
//	s := iter.Context2(iter.FromMap(map[int]string{1: "a", 2: "b"}), ctx)
func Context2[K, V any](s Seq2[K, V], ctx context.Context) Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if err := ctx.Err(); err != nil {
			return
		}
		s(func(k K, v V) bool {
			select {
			case <-ctx.Done():
				return false
			default:
				return yield(k, v)
			}
		})
	}
}

// ToChan converts a sequence to a channel.
// The channel is closed when the sequence is exhausted or context is cancelled.
//
// Example:
//
//	ctx := context.Background()
//	ch := iter.ToChan(iter.FromSlice([]int{1, 2, 3}), ctx)
//	for v := range ch {
//	  fmt.Println(v)
//	}
func ToChan[T any](s Seq[T], ctx context.Context) chan T {
	ch := make(chan T)
	go func() {
		defer close(ch)
		if err := ctx.Err(); err != nil {
			return
		}
		s(func(v T) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- v:
				return true
			}
		})
	}()
	return ch
}

// ToChan2 converts a key-value sequence to a channel of Pair pairs.
// The channel is closed when the sequence is exhausted or context is cancelled.
//
// Example:
//
//	ctx := context.Background()
//	ch := iter.ToChan2(iter.FromMap(map[int]string{1: "a", 2: "b"}), ctx)
//	for kv := range ch {
//	  fmt.Printf("%d: %s\n", kv.K, kv.V)
//	}
func ToChan2[K, V any](s Seq2[K, V], ctx context.Context) chan Pair[K, V] {
	ch := make(chan Pair[K, V])
	go func() {
		defer close(ch)
		if err := ctx.Err(); err != nil {
			return
		}
		s(func(k K, v V) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- Pair[K, V]{k, v}:
				return true
			}
		})
	}()
	return ch
}

// ToSlice collects all elements from the sequence into a slice.
//
// Example:
//
//	s := iter.FromSlice([]int{1, 2, 3})
//	sl := iter.ToSlice(s) // [1, 2, 3]
func ToSlice[T any](s Seq[T]) []T {
	out := make([]T, 0)
	s(func(v T) bool {
		out = append(out, v)
		return true
	})
	return out
}

// ToMap collects all key-value pairs into a map.
// Later pairs with the same key will overwrite earlier ones.
func ToMap[K comparable, V any](s Seq2[K, V]) map[K]V {
	m := make(map[K]V)
	s(func(k K, v V) bool {
		m[k] = v
		return true
	})
	return m
}

// ToPairs collects all key-value pairs into a slice of Pair structs.
func ToPairs[K, V any](s Seq2[K, V]) []Pair[K, V] {
	out := make([]Pair[K, V], 0)
	s(func(k K, v V) bool {
		out = append(out, Pair[K, V]{k, v})
		return true
	})
	return out
}
