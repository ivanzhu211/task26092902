package kbmatch

// maxBatch is the maximum number of entries per Import batch.
const maxBatch = 1000

// Import adds or replaces entries atomically per batch. If any row is
// invalid, the whole batch is rejected and all row errors are returned.
func (e *Engine) Import(entries []Entry) []RowError {
	if len(entries) > maxBatch {
		return []RowError{{Line: -1, Field: "batch", Reason: "batch exceeds 1000 entries"}}
	}
	var errs []RowError
	seenIDs := make(map[string]bool, len(entries))
	validated := make([]*validatedEntry, len(entries))
	for i := range entries {
		id := entries[i].ID
		validated[i] = validateEntry(&entries[i], i, seenIDs, &errs)
		if id != "" {
			seenIDs[id] = true
		}
	}
	if len(errs) > 0 {
		return errs
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ve := range validated {
		if old, ok := e.byID[ve.id]; ok {
			e.bySeq[old.seq] = nil
		}
		ent := &entry{id: ve.id, seq: len(e.bySeq), total: ve.total, need: ve.need}
		e.bySeq = append(e.bySeq, ent)
		e.byID[ve.id] = ent
		for _, t := range ve.terms {
			l := e.lists[t.key]
			if l == nil {
				l = &postingList{}
				e.lists[t.key] = l
			}
			delta := int16(t.weight - ve.need)
			l.append(posting{seq: int32(ent.seq), weight: int16(t.weight)}, delta)
		}
	}
	return nil
}

// Delete removes entries by id, ignoring unknown ids.
func (e *Engine) Delete(ids []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range ids {
		if ent, ok := e.byID[id]; ok {
			e.bySeq[ent.seq] = nil
			delete(e.byID, id)
		}
	}
}
