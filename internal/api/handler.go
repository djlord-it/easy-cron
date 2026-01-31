package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"easycron/internal/domain"
)

type Store interface {
	CreateJob(ctx context.Context, job domain.Job, schedule domain.Schedule) error
	ListJobs(ctx context.Context, projectID uuid.UUID) ([]JobWithSchedule, error)
	ListExecutions(ctx context.Context, jobID uuid.UUID) ([]domain.Execution, error)
}

type JobWithSchedule struct {
	Job      domain.Job
	Schedule domain.Schedule
}

type Handler struct {
	store     Store
	projectID uuid.UUID // single-tenant for now
}

func NewHandler(store Store, projectID uuid.UUID) *Handler {
	return &Handler{store: store, projectID: projectID}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/health" && r.Method == http.MethodGet:
		h.health(w, r)

	case path == "/jobs" && r.Method == http.MethodPost:
		h.createJob(w, r)

	case path == "/jobs" && r.Method == http.MethodGet:
		h.listJobs(w, r)

	case strings.HasSuffix(path, "/executions") && r.Method == http.MethodGet:
		h.listExecutions(w, r)

	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) createJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if err := validateCreateJob(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	timeout := 30 * time.Second
	if req.WebhookTimeout > 0 {
		timeout = time.Duration(req.WebhookTimeout) * time.Second
	}

	now := time.Now().UTC()
	jobID := uuid.New()
	scheduleID := uuid.New()

	job := domain.Job{
		ID:         jobID,
		ProjectID:  h.projectID,
		Name:       req.Name,
		Enabled:    true,
		ScheduleID: scheduleID,
		Delivery: domain.DeliveryConfig{
			Type:       domain.DeliveryTypeWebhook,
			WebhookURL: req.WebhookURL,
			Secret:     req.WebhookSecret,
			Timeout:    timeout,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	schedule := domain.Schedule{
		ID:             scheduleID,
		CronExpression: req.CronExpression,
		Timezone:       req.Timezone,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := h.store.CreateJob(r.Context(), job, schedule); err != nil {
		log.Printf("api: create job error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create job")
		return
	}

	resp := JobResponse{
		ID:             job.ID.String(),
		ProjectID:      job.ProjectID.String(),
		Name:           job.Name,
		Enabled:        job.Enabled,
		CronExpression: schedule.CronExpression,
		Timezone:       schedule.Timezone,
		WebhookURL:     job.Delivery.WebhookURL,
		CreatedAt:      formatTime(job.CreatedAt),
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.store.ListJobs(r.Context(), h.projectID)
	if err != nil {
		log.Printf("api: list jobs error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	resp := ListJobsResponse{Jobs: make([]JobResponse, len(jobs))}
	for i, jws := range jobs {
		resp.Jobs[i] = JobResponse{
			ID:             jws.Job.ID.String(),
			ProjectID:      jws.Job.ProjectID.String(),
			Name:           jws.Job.Name,
			Enabled:        jws.Job.Enabled,
			CronExpression: jws.Schedule.CronExpression,
			Timezone:       jws.Schedule.Timezone,
			WebhookURL:     jws.Job.Delivery.WebhookURL,
			CreatedAt:      formatTime(jws.Job.CreatedAt),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listExecutions(w http.ResponseWriter, r *http.Request) {
	// Extract job ID from path: /jobs/{id}/executions
	path := r.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "jobs" || parts[2] != "executions" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	jobID, err := uuid.Parse(parts[1])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	executions, err := h.store.ListExecutions(r.Context(), jobID)
	if err != nil {
		log.Printf("api: list executions error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list executions")
		return
	}

	resp := ListExecutionsResponse{Executions: make([]ExecutionResponse, len(executions))}
	for i, exec := range executions {
		resp.Executions[i] = ExecutionResponse{
			ID:          exec.ID.String(),
			JobID:       exec.JobID.String(),
			ScheduledAt: formatTime(exec.ScheduledAt),
			FiredAt:     formatTime(exec.FiredAt),
			Status:      string(exec.Status),
			CreatedAt:   formatTime(exec.CreatedAt),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
