package g

import (
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/enetx/g/cmp"
)

// All returns true only if fn returns true for every element.
// It stops early on the first false.
func (p SeqHeapPar[V]) All(fn func(V) bool) bool {
	var ok atomic.Bool
	ok.Store(true)

	p.Range(func(v V) bool {
		if !fn(v) {
			ok.Store(false)
			return false
		}
		return true
	})

	return ok.Load()
}

// Any returns true if fn returns true for any element.
// It stops early on the first true.
func (p SeqHeapPar[V]) Any(fn func(V) bool) bool {
	var ok atomic.Bool

	p.Range(func(v V) bool {
		if fn(v) {
			ok.Store(true)
			return false
		}
		return true
	})

	return ok.Load()
}

// Chain concatenates this SeqHeapPar with others, preserving full parallelism.
// Each sequence runs with its own worker pool in parallel.
func (p SeqHeapPar[V]) Chain(others ...SeqHeapPar[V]) SeqHeapPar[V] {
	return SeqHeapPar[V]{
		seq: func(yield func(V) bool) {
			done := make(chan struct{})
			result := make(chan V, int(p.workers)*4)

			var (
				wg   sync.WaitGroup
				once sync.Once
			)

			runSequence := func(seq SeqHeapPar[V]) {
				defer wg.Done()
				seq.Range(func(v V) bool {
					select {
					case <-done:
						return false
					case result <- v:
						return true
					}
				})
			}

			go func() {
				defer close(result)

				wg.Add(1)
				go runSequence(p)

				for _, o := range others {
					wg.Add(1)
					go runSequence(o)
				}

				wg.Wait()
			}()

			for {
				select {
				case <-done:
					return
				case v, ok := <-result:
					if !ok {
						return
					}
					if !yield(v) {
						once.Do(func() { close(done) })
						return
					}
				}
			}
		},
		workers: p.workers,
		process: func(v V) (V, bool) { return v, true },
	}
}

// Collect gathers all processed elements into a Heap with a custom comparison function.
func (p SeqHeapPar[V]) Collect(compareFn func(V, V) cmp.Ordering) *Heap[V] {
	ch := make(chan V)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			ch <- v
			return true
		})
	}()

	result := NewHeap(compareFn)
	for v := range ch {
		result.Push(v)
	}

	return result
}

// Count returns the total number of elements processed.
func (p SeqHeapPar[V]) Count() Int {
	var count atomic.Int64
	p.Range(func(V) bool {
		count.Add(1)
		return true
	})

	return Int(count.Load())
}

// Exclude removes elements for which fn returns true, in parallel.
func (p SeqHeapPar[V]) Exclude(fn func(V) bool) SeqHeapPar[V] {
	return p.Filter(func(v V) bool { return !fn(v) })
}

// Filter retains only elements where fn returns true.
func (p SeqHeapPar[V]) Filter(fn func(V) bool) SeqHeapPar[V] {
	prev := p.process

	return SeqHeapPar[V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(v V) (V, bool) {
			if mid, ok := prev(v); ok && fn(mid) {
				return mid, true
			}
			var zero V
			return zero, false
		},
	}
}

// FlatMap applies fn to each element in parallel, flattening the resulting sequences.
func (p SeqHeapPar[V]) FlatMap(fn func(V) SeqHeap[V]) SeqHeapPar[V] {
	return SeqHeapPar[V]{
		seq: func(yield func(V) bool) {
			done := make(chan struct{})
			result := make(chan V, 100)

			var (
				wg   sync.WaitGroup
				once sync.Once
			)

			go func() {
				defer close(result)

				p.Range(func(v V) bool {
					select {
					case <-done:
						return false
					default:
					}

					wg.Add(1)
					go func(val V) {
						defer wg.Done()
						fn(val)(func(item V) bool {
							select {
							case <-done:
								return false
							case result <- item:
								return true
							}
						})
					}(v)

					return true
				})

				wg.Wait()
			}()

			for {
				select {
				case <-done:
					return
				case v, ok := <-result:
					if !ok {
						return
					}
					if !yield(v) {
						once.Do(func() { close(done) })
						return
					}
				}
			}
		},
		workers: p.workers,
		process: func(v V) (V, bool) { return v, true },
	}
}

// FilterMap applies fn to each element in parallel, keeping only Some values.
func (p SeqHeapPar[V]) FilterMap(fn func(V) Option[V]) SeqHeapPar[V] {
	prev := p.process

	return SeqHeapPar[V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(v V) (V, bool) {
			if mid, ok := prev(v); ok {
				if opt := fn(mid); opt.IsSome() {
					return opt.Some(), true
				}
			}
			var zero V
			return zero, false
		},
	}
}

// StepBy yields every nth element.
func (p SeqHeapPar[V]) StepBy(n uint) SeqHeapPar[V] {
	if n == 0 {
		n = 1
	}

	prev := p.process
	counter := &atomic.Uint64{}

	return SeqHeapPar[V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(v V) (V, bool) {
			if mid, ok := prev(v); ok {
				count := counter.Add(1)
				if (count-1)%uint64(n) == 0 {
					return mid, true
				}
			}
			var zero V
			return zero, false
		},
	}
}

// MaxBy returns the maximum element according to the comparison function.
func (p SeqHeapPar[V]) MaxBy(fn func(V, V) cmp.Ordering) Option[V] {
	ch := make(chan V)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			ch <- v
			return true
		})
	}()

	var max V
	hasMax := false

	for v := range ch {
		if !hasMax {
			max = v
			hasMax = true
		} else if fn(v, max).IsGt() {
			max = v
		}
	}

	if hasMax {
		return Some(max)
	}
	return None[V]()
}

// MinBy returns the minimum element according to the comparison function.
func (p SeqHeapPar[V]) MinBy(fn func(V, V) cmp.Ordering) Option[V] {
	ch := make(chan V)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			ch <- v
			return true
		})
	}()

	var min V
	hasMin := false

	for v := range ch {
		if !hasMin {
			min = v
			hasMin = true
		} else if fn(v, min).IsLt() {
			min = v
		}
	}

	if hasMin {
		return Some(min)
	}
	return None[V]()
}

// Find returns the first element satisfying fn, or None if no such element exists.
func (p SeqHeapPar[V]) Find(fn func(V) bool) Option[V] {
	ch := make(chan V)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			if fn(v) {
				ch <- v
				return false
			}
			return true
		})
	}()

	if v, ok := <-ch; ok {
		return Some(v)
	}

	return None[V]()
}

// Fold reduces all elements into a single value, using fn to accumulate results.
// Note: This collects all processed elements first, then folds sequentially.
// The parallel processing happens during the Range phase.
func (p SeqHeapPar[V]) Fold(init V, fn func(acc, v V) V) V {
	ch := make(chan V)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			ch <- v
			return true
		})
	}()

	acc := init
	for v := range ch {
		acc = fn(acc, v)
	}

	return acc
}

// Flatten unpacks nested slices or arrays in the source, returning a flat parallel sequence.
func (p SeqHeapPar[V]) Flatten() SeqHeapPar[V] {
	seq := func(yield func(V) bool) {
		var recurse func(any) bool

		recurse = func(item any) bool {
			if item == nil {
				return true
			}

			rv := reflect.ValueOf(item)

			if !rv.IsValid() {
				return true
			}

			switch rv.Kind() {
			case reflect.Slice, reflect.Array:
				if rv.IsNil() {
					return true
				}

				for i := range rv.Len() {
					elem := rv.Index(i)

					if !elem.CanInterface() {
						continue
					}

					if !recurse(elem.Interface()) {
						return false
					}
				}
			default:
				if v, ok := item.(V); ok {
					if !yield(v) {
						return false
					}
				}
			}
			return true
		}

		resultsChan := make(chan V, 100)
		doneChan := make(chan struct{})
		var once sync.Once

		go func() {
			defer close(resultsChan)

			p.Range(func(v V) bool {
				select {
				case <-doneChan:
					return false
				default:
				}

				flattenedItems := flattenToSlice(v)
				for _, item := range flattenedItems {
					if flatItem, ok := item.(V); ok {
						select {
						case resultsChan <- flatItem:
						case <-doneChan:
							return false
						}
					}
				}
				return true
			})
		}()

		for {
			select {
			case v, ok := <-resultsChan:
				if !ok {
					return
				}
				if !yield(v) {
					once.Do(func() { close(doneChan) })
					return
				}
			case <-doneChan:
				return
			}
		}
	}

	return SeqHeapPar[V]{
		seq:     seq,
		workers: p.workers,
		process: func(v V) (V, bool) { return v, true },
	}
}

// Reduce aggregates elements of the parallel sequence using the provided function.
// The first received element is used as the initial accumulator.
// If the sequence is empty, returns None[V].
// Note: This collects all processed elements first, then reduces sequentially.
// The parallel processing happens during the Range phase.
func (p SeqHeapPar[V]) Reduce(fn func(a, b V) V) Option[V] {
	ch := make(chan V)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			ch <- v
			return true
		})
	}()

	var (
		acc   V
		first = true
	)

	for v := range ch {
		if first {
			acc = v
			first = false
			continue
		}

		acc = fn(acc, v)
	}

	if first {
		return None[V]()
	}

	return Some(acc)
}

// ForEach applies fn to each element without early exit.
func (p SeqHeapPar[V]) ForEach(fn func(V)) {
	p.Range(func(v V) bool {
		fn(v)
		return true
	})
}

// Inspect invokes fn on each element without altering the resulting sequence.
func (p SeqHeapPar[V]) Inspect(fn func(V)) SeqHeapPar[V] {
	prev := p.process

	return SeqHeapPar[V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(x V) (V, bool) {
			if mid, ok := prev(x); ok {
				fn(mid)
				return mid, true
			}
			var zero V
			return zero, false
		},
	}
}

// Map applies fn to each element.
func (p SeqHeapPar[V]) Map(fn func(V) V) SeqHeapPar[V] {
	prev := p.process

	return SeqHeapPar[V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(v V) (V, bool) {
			if mid, ok := prev(v); ok {
				return fn(mid), true
			}
			var zero V
			return zero, false
		},
	}
}

// Partition partitions elements using custom comparison functions for each heap.
func (p SeqHeapPar[V]) Partition(fn func(V) bool, leftCmp, rightCmp func(V, V) cmp.Ordering) (*Heap[V], *Heap[V]) {
	type item struct {
		value  V
		isLeft bool
	}

	ch := make(chan item)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			ch <- item{
				value:  v,
				isLeft: fn(v),
			}
			return true
		})
	}()

	left := NewHeap(leftCmp)
	right := NewHeap(rightCmp)

	for it := range ch {
		if it.isLeft {
			left.Push(it.value)
		} else {
			right.Push(it.value)
		}
	}

	return left, right
}

// Range applies fn to each processed element in parallel, stopping on false.
func (p SeqHeapPar[V]) Range(fn func(V) bool) {
	in := make(chan V)
	done := make(chan struct{})

	var (
		wg   sync.WaitGroup
		once sync.Once
	)

	go func() {
		defer close(in)
		p.seq(func(v V) bool {
			select {
			case <-done:
				return false
			case in <- v:
				return true
			}
		})
	}()

	wg.Add(int(p.workers))
	for range p.workers {
		go func() {
			defer wg.Done()
			for v := range in {
				if mid, ok := p.process(v); ok {
					if !fn(mid) {
						once.Do(func() { close(done) })
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}

func (p SeqHeapPar[V]) Skip(n uint) SeqHeapPar[V] {
	prev := p.process

	return SeqHeapPar[V]{
		seq: func(yield func(V) bool) {
			var cnt uint64
			p.seq(func(v V) bool {
				if atomic.AddUint64(&cnt, 1) > uint64(n) {
					return yield(v)
				}
				return true
			})
		},
		workers: p.workers,
		process: prev,
	}
}

func (p SeqHeapPar[V]) Take(n uint) SeqHeapPar[V] {
	prev := p.process

	return SeqHeapPar[V]{
		seq: func(yield func(V) bool) {
			var cnt uint64
			p.seq(func(v V) bool {
				if atomic.AddUint64(&cnt, 1) <= uint64(n) {
					return yield(v)
				}
				return false
			})
		},
		workers: p.workers,
		process: prev,
	}
}

// Unique removes duplicate elements, preserving the first occurrence.
func (p SeqHeapPar[V]) Unique() SeqHeapPar[V] {
	prev := p.process
	seen := NewMapSafe[any, struct{}]()

	return SeqHeapPar[V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(v V) (V, bool) {
			if mid, ok := prev(v); ok {
				if loaded := seen.Entry(mid).OrSet(struct{}{}); loaded.IsSome() {
					var zero V
					return zero, false
				}

				return mid, true
			}

			var zero V
			return zero, false
		},
	}
}
