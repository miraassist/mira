package agent

import (
	"context"
	"sync"
)

// WarmSlots keeps a local model's conversation prompt warm in one of two
// llama.cpp slots (docs/spec/warm-slot.md): the warm slot serves the
// conversation turns, the spare slot takes Mira's helpers and the reading of
// a new prompt. A reading runs only while nobody else uses the model; whoever
// comes stops it, and it starts again once the model is free.
type WarmSlots struct {
	mu      sync.Mutex
	warm    int
	busy    int
	pending bool
	reading context.CancelFunc
	// read starts the reading of the new prompt into the spare slot; set by
	// the conversation engine.
	read func()
}

// Warm is the slot of the conversation turns.
func (w *WarmSlots) Warm() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.warm
}

// Spare is the other slot.
func (w *WarmSlots) Spare() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return 1 - w.warm
}

// Enter is a turn's or a helper's use of the model: a running reading is
// stopped. Leave ends it; once nobody uses the model, a waiting reading
// starts.
func (w *WarmSlots) Enter() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.busy++
	if w.reading != nil {
		w.reading()
		w.reading = nil
		w.pending = true
	}
}

func (w *WarmSlots) Leave() {
	w.mu.Lock()
	w.busy--
	start := w.busy == 0 && w.pending && w.reading == nil && w.read != nil
	read := w.read
	w.mu.Unlock()
	if start {
		go read()
	}
}

// Changed notes that the prompt changed; with the model free, the reading
// starts at once.
func (w *WarmSlots) Changed() {
	w.mu.Lock()
	w.pending = true
	start := w.busy == 0 && w.reading == nil && w.read != nil
	read := w.read
	w.mu.Unlock()
	if start {
		go read()
	}
}

// OnRead sets what starts a reading.
func (w *WarmSlots) OnRead(read func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.read = read
}

// BeginReading starts a reading, if one waits and the model is free: its
// context ends when someone else needs the model.
func (w *WarmSlots) BeginReading(parent context.Context) (context.Context, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.pending || w.busy > 0 || w.reading != nil {
		return nil, 0, false
	}
	ctx, cancel := context.WithCancel(parent)
	w.pending, w.reading = false, cancel
	return ctx, 1 - w.warm, true
}

// EndReading ends a reading: once read, the spare slot is the warm one. A
// reading that was stopped waits again (Enter set it pending).
func (w *WarmSlots) EndReading(ctx context.Context, read bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() == nil {
		w.reading = nil
	}
	if read && ctx.Err() == nil {
		w.warm = 1 - w.warm
	}
}

// Warm is the slot keeper of a model with warm_slots, nil for any other.
func (r *Runtime) Warm(model string) *WarmSlots {
	m, ok := r.o.Config().Model(model)
	if !ok || !m.WarmSlots {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.warm == nil {
		r.warm = map[string]*WarmSlots{}
	}
	w, ok := r.warm[model]
	if !ok {
		w = &WarmSlots{}
		r.warm[model] = w
	}
	return w
}
