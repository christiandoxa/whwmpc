package motion

import (
	"testing"
	"time"
)

func TestSchedulerKeepsNewestDeadlineAndCancellation(t *testing.T) {
	s := NewScheduler()
	now := time.Now()
	for i := range 100 {
		s.Set(uint64(i), now.Add(time.Second))
	}
	if request := <-s.requests; request.generation != 99 {
		t.Fatalf("generation = %v", request)
	}
	s.Set(100, now)
	s.Set(101, time.Time{})
	if req := <-s.requests; req.generation != 101 || !req.next.IsZero() {
		t.Fatalf("cancellation = %v", req)
	}
}
