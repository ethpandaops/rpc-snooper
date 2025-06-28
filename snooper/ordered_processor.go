package snooper

import (
	"sync"
	"time"
)

type OrderedProcessor struct {
	mutex           sync.Mutex
	sequenceCounter uint64
	nextSequence    uint64
	activeSequences map[uint64]bool
	waiters         map[uint64]chan struct{}
	stopChan        chan struct{}
}

func NewOrderedProcessor(_ *Snooper) *OrderedProcessor {
	processor := &OrderedProcessor{
		sequenceCounter: 0,
		nextSequence:    1,
		activeSequences: make(map[uint64]bool),
		waiters:         make(map[uint64]chan struct{}),
		stopChan:        make(chan struct{}),
	}

	return processor
}

func (op *OrderedProcessor) Stop() {
	close(op.stopChan)
}

// GetNextSequence generates and returns the next sequence number, marking it as active
func (op *OrderedProcessor) GetNextSequence() uint64 {
	op.mutex.Lock()
	defer op.mutex.Unlock()

	op.sequenceCounter++
	seq := op.sequenceCounter
	op.activeSequences[seq] = true

	return seq
}

// WaitForSequence waits for the given sequence number to be processed.
// Returns true if sequence was reached, false if context was cancelled.
func (op *OrderedProcessor) WaitForSequence(sequence uint64) bool {
	op.mutex.Lock()

	// Skip any completed sequences and advance nextSequence
	op.skipCompletedSequences()

	// If this sequence is already ready, return immediately
	if sequence <= op.nextSequence {
		op.mutex.Unlock()
		return true
	}

	// Create a waiter channel for this sequence
	waiter := make(chan struct{})
	op.waiters[sequence] = waiter
	op.mutex.Unlock()

	// Wait for either the sequence to be ready or context cancellation
	select {
	case <-waiter:
		return true
	case <-op.stopChan:
		return false
	case <-time.After(5 * time.Second):
		return true
	}
}

// CompleteSequence marks a sequence as complete and wakes up waiters
func (op *OrderedProcessor) CompleteSequence(sequence uint64) {
	op.mutex.Lock()
	defer op.mutex.Unlock()

	// Remove from active sequences
	delete(op.activeSequences, sequence)

	// Skip completed sequences and wake up waiters
	op.skipCompletedSequences()
}

// skipCompletedSequences advances nextSequence past any completed sequences
// and wakes up any waiters. Must be called with mutex locked.
func (op *OrderedProcessor) skipCompletedSequences() {
	for op.nextSequence <= op.sequenceCounter && !op.activeSequences[op.nextSequence] {
		// This sequence is complete (or never existed), advance
		op.nextSequence++

		// Wake up the waiter for this sequence if it exists
		if waiter, exists := op.waiters[op.nextSequence]; exists {
			close(waiter)
			delete(op.waiters, op.nextSequence)
		}
	}
}
