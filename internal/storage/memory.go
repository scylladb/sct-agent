package storage

import (
	"sync"
	"time"
)

// Memory implements the Storage interface using in-memory storage
type Memory struct {
	jobs  sync.Map
	mutex sync.RWMutex
}

func NewMemory() *Memory {
	return &Memory{}
}

func (m *Memory) Save(job *Job) error {
	m.jobs.Store(job.ID, job)
	return nil
}

func (m *Memory) Get(id string) (*Job, bool) {
	if job, exists := m.jobs.Load(id); exists {
		return job.(*Job), true
	}
	return nil, false
}

func (m *Memory) List(status JobStatus, limit, offset int, since *time.Time) ([]*Job, int, error) {
	var allJobs []*Job

	m.jobs.Range(func(key, value interface{}) bool {
		job := value.(*Job)

		if status != "" && job.Status != status {
			return true // continue iteration
		}

		if since != nil && job.CreatedAt.Before(*since) {
			return true // continue iteration
		}

		allJobs = append(allJobs, job)
		return true
	})

	// pagination logic
	totalCount := len(allJobs)
	if offset > 0 {
		if offset >= len(allJobs) {
			return []*Job{}, totalCount, nil
		}
		allJobs = allJobs[offset:]
	}

	if limit > 0 && len(allJobs) > limit {
		allJobs = allJobs[:limit]
	}

	return allJobs, totalCount, nil
}

func (m *Memory) Delete(id string) error {
	m.jobs.Delete(id)
	return nil
}

func (m *Memory) Count() int {
	count := 0
	m.jobs.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

func (m *Memory) CountByStatus(status JobStatus) int {
	count := 0
	m.jobs.Range(func(_, value interface{}) bool {
		job := value.(*Job)
		if job.Status == status {
			count++
		}
		return true
	})
	return count
}

func (m *Memory) Cleanup(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	var toDelete []string

	m.jobs.Range(func(_, value interface{}) bool {
		job := value.(*Job)
		if job.CreatedAt.Before(cutoff) &&
			(job.Status == StatusCompleted || job.Status == StatusFailed || job.Status == StatusCancelled) {
			toDelete = append(toDelete, job.ID)
		}
		return true
	})

	for _, id := range toDelete {
		m.jobs.Delete(id)
	}

	return len(toDelete)
}
