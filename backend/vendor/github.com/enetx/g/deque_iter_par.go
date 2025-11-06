package g

import (
	"sync"
	"sync/atomic"

	"github.com/enetx/g/cmp"
)

// All returns true only if fn returns true for every element.
// It stops early on the first false.
func (p SeqDequePar[V]) All(fn func(V) bool) bool {
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
func (p SeqDequePar[V]) Any(fn func(V) bool) bool {
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

// Chain concatenates this SeqDequePar with others, preserving full parallelism.
// Each sequence runs with its own worker pool in parallel.
func (p SeqDequePar[V]) Chain(others ...SeqDequePar[V]) SeqDequePar[V] {
	return SeqDequePar[V]{
		seq: func(yield func(V) bool) {
			done := make(chan struct{})
			result := make(chan V, int(p.workers)*4)

			var (
				wg   sync.WaitGroup
				once sync.Once
			)

			runSequence := func(seq SeqDequePar[V]) {
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

// Collect gathers all processed elements into a Deque.
func (p SeqDequePar[V]) Collect() *Deque[V] {
	ch := make(chan V)

	go func() {
		defer close(ch)
		p.Range(func(v V) bool {
			ch <- v
			return true
		})
	}()

	result := NewDeque[V](0)
	for v := range ch {
		result.PushBack(v)
	}

	return result
}

// Count returns the total number of elements processed.
func (p SeqDequePar[V]) Count() Int {
	var count atomic.Int64
	p.Range(func(V) bool {
		count.Add(1)
		return true
	})

	return Int(count.Load())
}

// Exclude removes elements for which fn returns true, in parallel.
func (p SeqDequePar[V]) Exclude(fn func(V) bool) SeqDequePar[V] {
	return p.Filter(func(v V) bool { return !fn(v) })
}

// Filter retains only elements where fn returns true.
func (p SeqDequePar[V]) Filter(fn func(V) bool) SeqDequePar[V] {
	prev := p.process

	return SeqDequePar[V]{
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

// Find returns the first element satisfying fn, or None if no such element exists.
func (p SeqDequePar[V]) Find(fn func(V) bool) Option[V] {
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
func (p SeqDequePar[V]) Fold(init V, fn func(acc, v V) V) V {
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
func (p SeqDequePar[V]) Flatten() SeqDequePar[V] {
	seq := func(yield func(V) bool) {
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

	return SeqDequePar[V]{
		seq:     seq,
		workers: p.workers,
		process: func(v V) (V, bool) { return v, true },
	}
}

// FlatMap applies fn to each element in parallel, flattening the resulting sequences.
func (p SeqDequePar[V]) FlatMap(fn func(V) SeqDeque[V]) SeqDequePar[V] {
	return SeqDequePar[V]{
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
func (p SeqDequePar[V]) FilterMap(fn func(V) Option[V]) SeqDequePar[V] {
	prev := p.process

	return SeqDequePar[V]{
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
func (p SeqDequePar[V]) StepBy(n uint) SeqDequePar[V] {
	if n == 0 {
		n = 1
	}

	prev := p.process
	counter := &atomic.Uint64{}

	return SeqDequePar[V]{
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
func (p SeqDequePar[V]) MaxBy(fn func(V, V) cmp.Ordering) Option[V] {
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
func (p SeqDequePar[V]) MinBy(fn func(V, V) cmp.Ordering) Option[V] {
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

// Reduce aggregates elements of the parallel sequence using the provided function.
// The first received element is used as the initial accumulator.
// If the sequence is empty, returns None[V].
func (p SeqDequePar[V]) Reduce(fn func(a, b V) V) Option[V] {
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
func (p SeqDequePar[V]) ForEach(fn func(V)) {
	p.Range(func(v V) bool {
		fn(v)
		return true
	})
}

// Inspect invokes fn on each element without altering the resulting sequence.
func (p SeqDequePar[V]) Inspect(fn func(V)) SeqDequePar[V] {
	prev := p.process

	return SeqDequePar[V]{
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
func (p SeqDequePar[V]) Map(fn func(V) V) SeqDequePar[V] {
	prev := p.process

	return SeqDequePar[V]{
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

func (p SeqDequePar[V]) Partition(fn func(V) bool) (*Deque[V], *Deque[V]) {
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

	left, right := NewDeque[V](0), NewDeque[V](0)
	for it := range ch {
		if it.isLeft {
			left.PushBack(it.value)
		} else {
			right.PushBack(it.value)
		}
	}

	return left, right
}

// Range applies fn to each processed element in parallel, stopping on false.
func (p SeqDequePar[V]) Range(fn func(V) bool) {
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

func (p SeqDequePar[V]) Skip(n uint) SeqDequePar[V] {
	prev := p.process

	return SeqDequePar[V]{
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

func (p SeqDequePar[V]) Take(n uint) SeqDequePar[V] {
	prev := p.process

	return SeqDequePar[V]{
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
func (p SeqDequePar[V]) Unique() SeqDequePar[V] {
	prev := p.process
	seen := NewMapSafe[any, struct{}]()

	return SeqDequePar[V]{
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
