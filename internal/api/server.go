package api

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/scylladb/sct-agent/internal/executor"
	"github.com/scylladb/sct-agent/internal/storage"
)

type Server struct {
	executor  *executor.Executor
	apiKeys   []string
	version   string
	startTime time.Time
}

func New(executor *executor.Executor, apiKeys []string, version string) *Server {
	return &Server{
		executor:  executor,
		apiKeys:   apiKeys,
		version:   version,
		startTime: time.Now(),
	}
}

func (s *Server) SetupRoutes() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery(), LoggingMiddleware())

	r.GET("/health", s.healthHandler)

	protected := r.Group("/", AuthMiddleware(s.apiKeys))
	api := protected.Group("/api/v1")
	{
		api.POST("/commands", s.executeCommand)
		api.GET("/commands/:job_id", s.getCommand)
		api.GET("/commands", s.listCommands)
		api.DELETE("/commands/:job_id", s.cancelCommand)
	}

	return r
}

func (s *Server) executeCommand(c *gin.Context) {
	var req storage.ExecuteRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, storage.ErrorResponse{
			Error:   "Invalid request format",
			Message: err.Error(),
		})
		return
	}

	if req.Command == "" {
		c.JSON(http.StatusBadRequest, storage.ErrorResponse{
			Error:   "Missing required field",
			Message: "Command field is required",
		})
		return
	}

	job, err := s.executor.Execute(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, storage.ErrorResponse{
			Error:   "Execution failed",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, storage.ExecuteResponse{
		JobID:     job.ID,
		Status:    job.Status,
		CreatedAt: job.CreatedAt,
		Command:   job.Command,
		Message:   "Command queued successfully",
	})
}

// handles GET /api/v1/commands/{job_id}
func (s *Server) getCommand(c *gin.Context) {
	jobID := c.Param("job_id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, storage.ErrorResponse{
			Error:   "Missing job ID",
			Message: "Job ID parameter is required",
		})
		return
	}

	if job, err := s.executor.GetJob(jobID); err != nil {
		c.JSON(http.StatusNotFound, storage.ErrorResponse{
			Error:   "Job not found",
			Message: fmt.Sprintf("Job with ID %s not found", jobID),
		})
	} else {
		c.JSON(http.StatusOK, job)
	}
}

func parseQueryParam(param string, defaultValue, maxValue int) int {
	value, err := strconv.Atoi(param)
	if err != nil || value < 0 {
		return defaultValue
	}
	if maxValue > 0 && value > maxValue {
		return maxValue
	}
	return value
}

// handles GET /api/v1/commands
func (s *Server) listCommands(c *gin.Context) {
	limit := parseQueryParam(c.DefaultQuery("limit", "50"), 50, 500)
	offset := parseQueryParam(c.DefaultQuery("offset", "0"), 0, -1)

	var status storage.JobStatus
	if statusParam := c.Query("status"); statusParam != "" {
		status = storage.JobStatus(statusParam)
	}

	var since *time.Time
	if sinceParam := c.Query("since"); sinceParam != "" {
		if t, err := time.Parse(time.RFC3339, sinceParam); err == nil {
			since = &t
		}
	}

	jobs, total, err := s.executor.ListJobs(status, limit, offset, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, storage.ErrorResponse{
			Error:   "Failed to list jobs",
			Message: err.Error(),
		})
		return
	}

	jobList := make([]storage.Job, len(jobs))
	for i, job := range jobs {
		jobList[i] = *job
	}

	c.JSON(http.StatusOK, storage.JobListResponse{
		Commands: jobList,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// handles DELETE /api/v1/commands/{job_id}
func (s *Server) cancelCommand(c *gin.Context) {
	jobID := c.Param("job_id")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, storage.ErrorResponse{
			Error:   "Missing job ID",
			Message: "Job ID parameter is required",
		})
		return
	}

	if err := s.executor.CancelJob(jobID); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, storage.ErrorResponse{
			Error:   "Cannot cancel job",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"job_id":  jobID,
		"status":  "cancelled",
		"message": "Command cancelled successfully",
	})
}

// handles GET /health
func (s *Server) healthHandler(c *gin.Context) {
	stats := s.executor.GetStats()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// TODO: MVP purposes always returns "healthy". Later actual health checks can be implemented:
	// - memory thresholds
	// - executor capacity (e.g. degradation if at max concurrent jobs)
	// - etc
	response := storage.HealthResponse{
		Status:        "healthy",
		Version:       s.version,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		RunningJobs:   stats["running"],
		CompletedJobs: stats["completed"],
		System: map[string]interface{}{
			"memory_usage_mb": memStats.Alloc / (1024 * 1024),
		},
	}

	c.JSON(http.StatusOK, response)
}
