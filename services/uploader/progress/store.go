package progress

import (
	"sync"
	"time"

	"brodaty.dev/cv-platform/uploader/queue"
)

const (
	retentionPeriod = 24 * time.Hour
	maxJobs         = 10000
)

type jobState struct {
	event       queue.StatusEvent
	subscribers map[chan queue.StatusEvent]struct{}
}

type Store struct {
	mu   sync.Mutex
	jobs map[string]*jobState
}

func NewStore() *Store {
	return &Store{jobs: make(map[string]*jobState)}
}

func (s *Store) Update(event queue.StatusEvent) bool {
	if event.JobID == "" || !validStage(event.Stage) {
		return false
	}
	if event.UpdatedAt.IsZero() {
		event.UpdatedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireOldJobs(time.Now())

	state, exists := s.jobs[event.JobID]
	if exists {
		if terminal(state.event.Stage) || event.Stage == "queued" {
			return false
		}
		state.event = event
	} else {
		if len(s.jobs) >= maxJobs {
			s.evictOldestJob()
		}
		state = &jobState{
			event:       event,
			subscribers: make(map[chan queue.StatusEvent]struct{}),
		}
		s.jobs[event.JobID] = state
	}

	for subscriber := range state.subscribers {
		select {
		case subscriber <- event:
		default:
			delete(state.subscribers, subscriber)
			close(subscriber)
		}
	}
	return true
}

func (s *Store) Subscribe(jobID string) (queue.StatusEvent, <-chan queue.StatusEvent, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.jobs[jobID]
	if !exists || time.Since(state.event.UpdatedAt) > retentionPeriod {
		if exists {
			s.removeJob(jobID)
		}
		return queue.StatusEvent{}, nil, nil, false
	}

	updates := make(chan queue.StatusEvent, 16)
	state.subscribers[updates] = struct{}{}
	unsubscribe := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current, exists := s.jobs[jobID]; exists {
			if _, subscribed := current.subscribers[updates]; subscribed {
				delete(current.subscribers, updates)
				close(updates)
			}
		}
	}
	return state.event, updates, unsubscribe, true
}

func (s *Store) expireOldJobs(now time.Time) {
	for jobID, state := range s.jobs {
		if now.Sub(state.event.UpdatedAt) > retentionPeriod {
			s.removeJob(jobID)
		}
	}
}

func (s *Store) evictOldestJob() {
	var oldestID string
	var oldestTime time.Time
	for jobID, state := range s.jobs {
		if oldestID == "" || state.event.UpdatedAt.Before(oldestTime) {
			oldestID = jobID
			oldestTime = state.event.UpdatedAt
		}
	}
	if oldestID != "" {
		s.removeJob(oldestID)
	}
}

func (s *Store) removeJob(jobID string) {
	state := s.jobs[jobID]
	for subscriber := range state.subscribers {
		delete(state.subscribers, subscriber)
		close(subscriber)
	}
	delete(s.jobs, jobID)
}

func validStage(stage string) bool {
	switch stage {
	case "queued", "processing", "retrying", "completed", "failed":
		return true
	default:
		return false
	}
}

func terminal(stage string) bool {
	return stage == "completed" || stage == "failed"
}
