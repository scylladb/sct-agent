package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStorage(t *testing.T) {
	storage := NewMemory()

	// test Save and Get
	job := &Job{
		ID:        "test-job-1",
		Command:   "echo",
		Args:      []string{"hello"},
		Status:    StatusQueued,
		CreatedAt: time.Now(),
	}
	require.NoError(t, storage.Save(job))

	retrieved, exists := storage.Get("test-job-1")
	assert.True(t, exists)
	assert.Equal(t, job, retrieved)

	// test non-existent job
	_, exists = storage.Get("non-existent")
	assert.False(t, exists)

	// test Count
	assert.Equal(t, 1, storage.Count())

	// test CountByStatus
	assert.Equal(t, 1, storage.CountByStatus(StatusQueued))
	assert.Equal(t, 0, storage.CountByStatus(StatusCompleted))

	// test List
	jobs, total, err := storage.List("", 10, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "test-job-1", jobs[0].ID)

	// test List with status filter
	jobs, total, err = storage.List(StatusCompleted, 10, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, jobs)

	// test Delete
	require.NoError(t, storage.Delete("test-job-1"))
	assert.Equal(t, 0, storage.Count())
	_, exists = storage.Get("test-job-1")
	assert.False(t, exists)
}

func TestMemoryStorageList(t *testing.T) {
	storage := NewMemory()

	jobs := []*Job{
		{ID: "job-1", Command: "echo", Status: StatusCompleted, CreatedAt: time.Now().Add(-2 * time.Hour)},
		{ID: "job-2", Command: "sleep", Status: StatusRunning, CreatedAt: time.Now().Add(-1 * time.Hour)},
		{ID: "job-3", Command: "ls", Status: StatusCompleted, CreatedAt: time.Now()},
	}

	for _, job := range jobs {
		require.NoError(t, storage.Save(job))
	}

	assertList := func(status JobStatus, limit, offset int, since *time.Time, expectedTotal, expectedLen int) {
		result, total, err := storage.List(status, limit, offset, since)
		require.NoError(t, err)
		assert.Equal(t, expectedTotal, total)
		assert.Equal(t, expectedLen, len(result))
	}

	// test pagination
	assertList("", 2, 0, nil, 3, 2)

	// test offset
	assertList("", 2, 1, nil, 3, 2)

	// test status filter
	assertList(StatusCompleted, 10, 0, nil, 2, 2)

	// test since filter
	since := time.Now().Add(-30 * time.Minute)
	assertList("", 10, 0, &since, 1, 1)
	result, _, _ := storage.List("", 10, 0, &since)
	assert.Equal(t, "job-3", result[0].ID)
}

func TestMemoryStorageCleanup(t *testing.T) {
	storage := NewMemory()

	jobs := []*Job{
		{ID: "old-job", Command: "echo", Status: StatusCompleted, CreatedAt: time.Now().Add(-25 * time.Hour)},
		{ID: "recent-job", Command: "echo", Status: StatusCompleted, CreatedAt: time.Now().Add(-1 * time.Hour)},
		{ID: "running-job", Command: "sleep", Status: StatusRunning, CreatedAt: time.Now().Add(-25 * time.Hour)},
	}

	for _, job := range jobs {
		require.NoError(t, storage.Save(job))
	}

	assert.Equal(t, 3, storage.Count())

	// cleanup jobs older than 24 hours
	cleaned := storage.Cleanup(24 * time.Hour)
	assert.Equal(t, 1, cleaned) // only old completed job should be cleaned
	assert.Equal(t, 2, storage.Count())

	// verify remaining jobs
	_, exists := storage.Get("old-job")
	assert.False(t, exists, "Old completed job should be cleaned up")

	_, exists = storage.Get("recent-job")
	assert.True(t, exists, "Recent job should remain")

	_, exists = storage.Get("running-job")
	assert.True(t, exists, "Running job should remain regardless of age")
}
