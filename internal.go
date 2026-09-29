package kbmatch

const (
	// blockSize is the number of postings per skip block.
	blockSize = 64
	// superBlocks is the number of blocks per superblock.
	superBlocks = 256
)

// entry is the internal representation of a live entry.
type entry struct {
	id    string
	seq   int
	total int
	need  int
}

// posting is one (entry, weight) hit inside a posting list.
type posting struct {
	seq    int32
	weight int16
}

// postingList holds postings sorted by seq plus block-level max-delta
// skip structures. A posting's delta (weight minus its entry's need) is
// immutable, so the maxima stay valid upper bounds even after deletions.
type postingList struct {
	postings []posting
	blockMax []int16
	superMax []int16
}

// append adds a posting (seqs must be appended in increasing order) and
// updates the block maxima with the posting's delta.
func (l *postingList) append(p posting, delta int16) {
	l.postings = append(l.postings, p)
	b := (len(l.postings) - 1) / blockSize
	if b >= len(l.blockMax) {
		l.blockMax = append(l.blockMax, delta)
	} else if delta > l.blockMax[b] {
		l.blockMax[b] = delta
	}
	sb := b / superBlocks
	if sb >= len(l.superMax) {
		l.superMax = append(l.superMax, delta)
	} else if delta > l.superMax[sb] {
		l.superMax[sb] = delta
	}
}
