package g

import (
	"sync"
	"sync/atomic"
)

// All returns true if fn returns true for every pair.
func (p SeqMapPar[K, V]) All(fn func(K, V) bool) bool {
	var ok atomic.Bool
	ok.Store(true)

	p.Range(func(k K, v V) bool {
		if !fn(k, v) {
			ok.Store(false)
			return false
		}
		return true
	})

	return ok.Load()
}

// Any returns true if fn returns true for any pair.
func (p SeqMapPar[K, V]) Any(fn func(K, V) bool) bool {
	var ok atomic.Bool

	p.Range(func(k K, v V) bool {
		if fn(k, v) {
			ok.Store(true)
			return false
		}
		return true
	})

	return ok.Load()
}

// Chain concatenates this SeqMapPar with others, preserving full parallelism.
// Each sequence runs with its own worker pool in parallel..
func (p SeqMapPar[K, V]) Chain(others ...SeqMapPar[K, V]) SeqMapPar[K, V] {
	return SeqMapPar[K, V]{
		seq: func(yield func(K, V) bool) {
			done := make(chan struct{})
			result := make(chan Pair[K, V], 100)

			var (
				wg   sync.WaitGroup
				once sync.Once
			)

			runSequence := func(seq SeqMapPar[K, V]) {
				defer wg.Done()
				seq.Range(func(k K, v V) bool {
					select {
					case <-done:
						return false
					case result <- Pair[K, V]{Key: k, Value: v}:
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
				case pair, ok := <-result:
					if !ok {
						return
					}
					if !yield(pair.Key, pair.Value) {
						once.Do(func() { close(done) })
						return
					}
				}
			}
		},
		workers: p.workers,
		process: func(pair Pair[K, V]) (Pair[K, V], bool) { return pair, true },
	}
}

// Collect gathers all processed pairs into a Map.
func (p SeqMapPar[K, V]) Collect() Map[K, V] {
	ch := make(chan Pair[K, V])

	go func() {
		defer close(ch)
		p.Range(func(k K, v V) bool {
			ch <- Pair[K, V]{Key: k, Value: v}
			return true
		})
	}()

	m := NewMap[K, V]()
	for pair := range ch {
		m.Set(pair.Key, pair.Value)
	}

	return m
}

// Count returns the total number of processed pairs.
func (p SeqMapPar[K, V]) Count() Int {
	var cnt atomic.Int64

	p.Range(func(_ K, _ V) bool {
		cnt.Add(1)
		return true
	})

	return Int(cnt.Load())
}

// Exclude removes pairs where fn returns true.
func (p SeqMapPar[K, V]) Exclude(fn func(K, V) bool) SeqMapPar[K, V] {
	return p.Filter(func(k K, v V) bool { return !fn(k, v) })
}

// Filter retains only pairs where fn returns true.
func (p SeqMapPar[K, V]) Filter(fn func(K, V) bool) SeqMapPar[K, V] {
	prev := p.process

	return SeqMapPar[K, V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(pair Pair[K, V]) (Pair[K, V], bool) {
			if mid, ok := prev(pair); ok && fn(mid.Key, mid.Value) {
				return mid, true
			}
			return Pair[K, V]{}, false
		},
	}
}

// Find returns the first pair matching fn, or a zero Option if none.
func (p SeqMapPar[K, V]) Find(fn func(K, V) bool) Option[Pair[K, V]] {
	ch := make(chan Pair[K, V])

	go func() {
		defer close(ch)
		p.Range(func(k K, v V) bool {
			if fn(k, v) {
				ch <- Pair[K, V]{Key: k, Value: v}
				return false
			}
			return true
		})
	}()

	if pair, ok := <-ch; ok {
		return Some(pair)
	}

	return None[Pair[K, V]]()
}

// ForEach invokes fn on each key/value pair for side-effects,
// processing all pairs in parallel without early exit.
func (p SeqMapPar[K, V]) ForEach(fn func(K, V)) {
	p.Range(func(k K, v V) bool {
		fn(k, v)
		return true
	})
}

// Inspect invokes fn on each key/value pair for side-effects,
// without modifying the resulting sequence.
func (p SeqMapPar[K, V]) Inspect(fn func(K, V)) SeqMapPar[K, V] {
	prev := p.process

	return SeqMapPar[K, V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(pair Pair[K, V]) (Pair[K, V], bool) {
			if mid, ok := prev(pair); ok {
				fn(mid.Key, mid.Value)
				return mid, true
			}
			return Pair[K, V]{}, false
		},
	}
}

// Map applies transform to each pair.
func (p SeqMapPar[K, V]) Map(transform func(K, V) (K, V)) SeqMapPar[K, V] {
	prev := p.process

	return SeqMapPar[K, V]{
		seq:     p.seq,
		workers: p.workers,
		process: func(pair Pair[K, V]) (Pair[K, V], bool) {
			if mid, ok := prev(pair); ok {
				k2, v2 := transform(mid.Key, mid.Value)
				return Pair[K, V]{Key: k2, Value: v2}, true
			}
			return Pair[K, V]{}, false
		},
	}
}

// Range applies fn to each processed pair in parallel, stopping early if fn returns false.
func (p SeqMapPar[K, V]) Range(fn func(K, V) bool) {
	in := make(chan Pair[K, V])
	done := make(chan struct{})

	var (
		wg   sync.WaitGroup
		once sync.Once
	)

	go func() {
		defer close(in)
		p.seq(func(k K, v V) bool {
			select {
			case <-done:
				return false
			case in <- Pair[K, V]{Key: k, Value: v}:
				return true
			}
		})
	}()

	wg.Add(int(p.workers))
	for range p.workers {
		go func() {
			defer wg.Done()
			for pair := range in {
				if mid, ok := p.process(pair); ok {
					if !fn(mid.Key, mid.Value) {
						once.Do(func() { close(done) })
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}

// Skip drops the first n pairs.
func (p SeqMapPar[K, V]) Skip(n Int) SeqMapPar[K, V] {
	prev := p.process

	return SeqMapPar[K, V]{
		seq: func(yield func(K, V) bool) {
			var cnt int64
			p.seq(func(k K, v V) bool {
				if atomic.AddInt64(&cnt, 1) > int64(n) {
					return yield(k, v)
				}
				return true
			})
		},
		workers: p.workers,
		process: prev,
	}
}

// Take yields at most n pairs.
func (p SeqMapPar[K, V]) Take(n Int) SeqMapPar[K, V] {
	prev := p.process

	return SeqMapPar[K, V]{
		seq: func(yield func(K, V) bool) {
			var cnt int64
			p.seq(func(k K, v V) bool {
				if atomic.AddInt64(&cnt, 1) <= int64(n) {
					return yield(k, v)
				}
				return false
			})
		},
		workers: p.workers,
		process: prev,
	}
}
