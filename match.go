package kbmatch

import (
	"errors"
	"unicode/utf8"
)

// maxMessageRunes is the maximum message length in code points.
const maxMessageRunes = 2000

// ErrMessageTooLong is returned by Match for messages over 2000 runes.
var ErrMessageTooLong = errors.New("kbmatch: message exceeds 2000 runes")

// iterator walks one posting list in increasing seq order. Invariant:
// idx either equals len(postings) or points at a live entry's posting.
type iterator struct {
	list *postingList
	idx  int
}

func (it *iterator) seq() int { return int(it.list.postings[it.idx].seq) }

func (it *iterator) exhausted() bool { return it.idx >= len(it.list.postings) }

// matchLocked performs the actual matching; caller must hold at least the
// read lock.
func (e *Engine) matchLocked(text string) (Result, bool, error) {
	if utf8.RuneCountInString(text) > maxMessageRunes {
		return Result{}, false, ErrMessageTooLong
	}
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return Result{}, false, nil
	}
	seen := make(map[string]struct{})
	var iters []*iterator
	n := len(tokens)
	for i := 0; i < n; i++ {
		key := ""
		for l := 1; l <= 8 && i+l <= n; l++ {
			if l > 1 {
				key += "\x00"
			}
			key += tokens[i+l-1]
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			lst := e.lists[key]
			if lst == nil {
				continue
			}
			it := &iterator{list: lst, idx: e.nextLive(lst, 0)}
			if !it.exhausted() {
				iters = append(iters, it)
			}
		}
	}
	var best *entry
	bestHit := 0
	for len(iters) > 0 {
		minSeq := -1
		for _, it := range iters {
			if minSeq < 0 || it.seq() < minSeq {
				minSeq = it.seq()
			}
		}
		g := -1 // second smallest seq, -1 means "no bound"
		wsum := 0
		count := 0
		var solo *iterator
		for _, it := range iters {
			if s := it.seq(); s == minSeq {
				wsum += int(it.list.postings[it.idx].weight)
				count++
				solo = it
			} else if g < 0 || s < g {
				g = s
			}
		}
		ent := e.bySeq[minSeq]
		if wsum >= ent.need {
			if best == nil || wsum*best.total > bestHit*ent.total ||
				(wsum*best.total == bestHit*ent.total && ent.id < best.id) {
				best, bestHit = ent, wsum
			}
		}
		if count == 1 {
			solo.idx = e.nextLive(solo.list, solo.idx+1)
			e.skipAlone(solo, g)
		} else {
			for _, it := range iters {
				if it.seq() == minSeq {
					it.idx = e.nextLive(it.list, it.idx+1)
				}
			}
		}
		kept := iters[:0]
		for _, it := range iters {
			if !it.exhausted() {
				kept = append(kept, it)
			}
		}
		iters = kept
	}
	if best == nil {
		return Result{}, false, nil
	}
	return Result{ID: best.id, HitWeight: bestHit, TotalWeight: best.total}, true, nil
}

// nextLive advances idx past postings of deleted entries.
func (e *Engine) nextLive(l *postingList, idx int) int {
	for idx < len(l.postings) && e.bySeq[l.postings[idx].seq] == nil {
		idx++
	}
	return idx
}

// skipAlone bulk-advances an iterator that is the only one able to cover
// docs below bound g: such an entry can pass only if its own weight
// reaches its need, so stretches with negative delta are skipped using
// the block maxima. A negative g means unbounded.
func (e *Engine) skipAlone(it *iterator, g int) {
	l := it.list
	posts := l.postings
	for it.idx < len(posts) {
		p := &posts[it.idx]
		if g >= 0 && int(p.seq) >= g {
			break
		}
		if ent := e.bySeq[p.seq]; ent != nil && int(p.weight) >= ent.need {
			break
		}
		b := it.idx / blockSize
		sb := b / superBlocks
		if l.superMax[sb] < 0 {
			end := (sb + 1) * superBlocks * blockSize
			if end > len(posts) {
				end = len(posts)
			}
			if g < 0 || int(posts[end-1].seq) < g {
				it.idx = end
				continue
			}
		}
		if l.blockMax[b] < 0 {
			end := (b + 1) * blockSize
			if end > len(posts) {
				end = len(posts)
			}
			if g < 0 || int(posts[end-1].seq) < g {
				it.idx = end
				continue
			}
		}
		it.idx++
	}
	it.idx = e.nextLive(l, it.idx)
}
