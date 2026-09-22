package parking

import (
	"sync"
	"testing"
	"time"
)

func TestLockManagerSerializesSameSlot(t *testing.T) {
	m := NewLockManager()
	var wg sync.WaitGroup
	value := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := m.Lock("slot-a")
			current := value
			time.Sleep(time.Microsecond)
			value = current + 1
			unlock()
		}()
	}
	wg.Wait()
	if value != 100 {
		t.Fatalf("value = %d, want 100", value)
	}
}

func TestLockManagerAllowsDifferentSlots(t *testing.T) {
	m := NewLockManager()
	a := m.Lock("a")
	done := make(chan struct{})
	go func() {
		b := m.Lock("b")
		b()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("different slot lock was unexpectedly blocked")
	}
	a()
}
