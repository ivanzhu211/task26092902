package kbengine

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

// TestConcurrentMatchImportDelete hammers Match from many goroutines while a
// writer imports, replaces and deletes batches. Run with -race.
func TestConcurrentMatchImportDelete(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	v := newVocabGen(rand.New(rand.NewSource(8)))
	kb := New()

	// Seed the library.
	for b := 0; b < 5; b++ {
		var batch []Entry
		for i := 0; i < 200; i++ {
			batch = append(batch, v.makeEntry(fmt.Sprintf("seed-%d-%d", b, i), "账单", false))
		}
		if errs := kb.Import(batch); errs != nil {
			t.Fatalf("seed import: %v", errs[:2])
		}
	}

	const matchers = 6
	var wg sync.WaitGroup
	stop := make(chan struct{})
	errCh := make(chan error, matchers)
	for g := 0; g < matchers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			local := rand.New(rand.NewSource(int64(100 + g)))
			lv := newVocabGen(local)
			for {
				select {
				case <-stop:
					return
				default:
				}
				msg := lv.message(nil, "账单", true)
				if _, err := kb.Match(msg); err != nil {
					errCh <- err
					return
				}
			}
		}(g)
	}

	// Writer: batches of import/replace/delete.
	for round := 0; round < 60; round++ {
		var batch []Entry
		usedID := map[string]bool{}
		for i := 0; i < 50; i++ {
			id := fmt.Sprintf("w-%d-%d", round, i)
			if round > 0 && rng.Intn(3) == 0 {
				id = fmt.Sprintf("w-%d-%d", rng.Intn(round), rng.Intn(50))
			}
			if usedID[id] {
				continue
			}
			usedID[id] = true
			batch = append(batch, v.makeEntry(id, "账单", rng.Intn(2) == 0))
		}
		if errs := kb.Import(batch); errs != nil {
			t.Fatalf("import round %d: %v", round, errs)
		}
		if round%3 == 0 {
			var ids []string
			for i := 0; i < 20; i++ {
				ids = append(ids, fmt.Sprintf("w-%d-%d", rng.Intn(round+1), rng.Intn(50)))
			}
			kb.Delete(ids)
		}
	}
	close(stop)
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatalf("match error: %v", err)
	default:
	}
}
