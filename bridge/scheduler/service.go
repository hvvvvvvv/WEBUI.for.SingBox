package scheduler

import (
	"context"
	"log/slog"
	"time"

	"guiforcores/bridge/logging"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Backend interface {
	StartScheduler()
	StopScheduler()
	ListScheduledTasks(context.Context, *connect.Request[appv1.ListScheduledTasksRequest]) (*connect.Response[appv1.ListScheduledTasksResponse], error)
	CreateScheduledTask(context.Context, *connect.Request[appv1.CreateScheduledTaskRequest]) (*connect.Response[appv1.CreateScheduledTaskResponse], error)
	UpdateScheduledTask(context.Context, *connect.Request[appv1.UpdateScheduledTaskRequest]) (*connect.Response[appv1.UpdateScheduledTaskResponse], error)
	DeleteScheduledTask(context.Context, *connect.Request[appv1.DeleteScheduledTaskRequest]) (*connect.Response[appv1.DeleteScheduledTaskResponse], error)
	ReorderScheduledTasks(context.Context, *connect.Request[appv1.ReorderScheduledTasksRequest]) (*connect.Response[appv1.ReorderScheduledTasksResponse], error)
	RunScheduledTask(context.Context, *connect.Request[appv1.RunScheduledTaskRequest]) (*connect.Response[appv1.RunScheduledTaskResponse], error)
	ListScheduledTaskLogs(context.Context, *connect.Request[appv1.ListScheduledTaskLogsRequest]) (*connect.Response[appv1.ListScheduledTaskLogsResponse], error)
	ClearScheduledTaskLogs(context.Context, *connect.Request[appv1.ClearScheduledTaskLogsRequest]) (*connect.Response[appv1.ClearScheduledTaskLogsResponse], error)
	NextScheduledTaskRuns(context.Context, *connect.Request[appv1.NextScheduledTaskRunsRequest]) (*connect.Response[appv1.NextScheduledTaskRunsResponse], error)
}

type Service struct {
	backend Backend
}

func NewService(backend Backend) *Service {
	return &Service{backend: backend}
}

func (s *Service) Start() {
	s.backend.StartScheduler()
	slog.Info("scheduler started", "component", "scheduler", "operation", "start", "result", "success")
}

func (s *Service) Stop() {
	s.backend.StopScheduler()
	slog.Info("scheduler stopped", "component", "scheduler", "operation", "stop", "result", "success")
}

func (s *Service) ListScheduledTasks(ctx context.Context, req *connect.Request[appv1.ListScheduledTasksRequest]) (*connect.Response[appv1.ListScheduledTasksResponse], error) {
	return s.backend.ListScheduledTasks(ctx, req)
}

func (s *Service) CreateScheduledTask(ctx context.Context, req *connect.Request[appv1.CreateScheduledTaskRequest]) (*connect.Response[appv1.CreateScheduledTaskResponse], error) {
	started := time.Now()
	response, err := s.backend.CreateScheduledTask(ctx, req)
	logging.Complete(ctx, "scheduler", "create", "scheduled task created", started, err, "payload_bytes", len(req.Msg.GetTaskJson()))
	return response, err
}

func (s *Service) UpdateScheduledTask(ctx context.Context, req *connect.Request[appv1.UpdateScheduledTaskRequest]) (*connect.Response[appv1.UpdateScheduledTaskResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateScheduledTask(ctx, req)
	logging.Complete(ctx, "scheduler", "update", "scheduled task updated", started, err, "payload_bytes", len(req.Msg.GetTaskJson()))
	return response, err
}

func (s *Service) DeleteScheduledTask(ctx context.Context, req *connect.Request[appv1.DeleteScheduledTaskRequest]) (*connect.Response[appv1.DeleteScheduledTaskResponse], error) {
	started := time.Now()
	response, err := s.backend.DeleteScheduledTask(ctx, req)
	logging.Complete(ctx, "scheduler", "delete", "scheduled task deleted", started, err, "task_id", req.Msg.GetId())
	return response, err
}

func (s *Service) ReorderScheduledTasks(ctx context.Context, req *connect.Request[appv1.ReorderScheduledTasksRequest]) (*connect.Response[appv1.ReorderScheduledTasksResponse], error) {
	started := time.Now()
	response, err := s.backend.ReorderScheduledTasks(ctx, req)
	logging.Complete(ctx, "scheduler", "reorder", "scheduled tasks reordered", started, err, "total", len(req.Msg.GetIds()))
	return response, err
}

func (s *Service) RunScheduledTask(ctx context.Context, req *connect.Request[appv1.RunScheduledTaskRequest]) (*connect.Response[appv1.RunScheduledTaskResponse], error) {
	started := time.Now()
	response, err := s.backend.RunScheduledTask(ctx, req)
	total := 0
	var results []*appv1.TaskResult
	if response != nil {
		results = response.Msg.GetResults()
		total = len(results)
	}
	successes, failures := scheduledTaskResultCounts(results)
	attrs := []any{"task_id", req.Msg.GetId(), "total", total, "success_count", successes, "failure_count", failures}
	if err == nil && failures > 0 {
		logging.Partial(ctx, "scheduler", "run", "scheduled task completed with failures", started, attrs...)
	} else {
		logging.Complete(ctx, "scheduler", "run", "scheduled task completed", started, err, attrs...)
	}
	return response, err
}

func scheduledTaskResultCounts(results []*appv1.TaskResult) (int, int) {
	var successes, failures int
	for _, result := range results {
		if result.GetOk() {
			successes++
		} else {
			failures++
		}
	}
	return successes, failures
}

func (s *Service) ListScheduledTaskLogs(ctx context.Context, req *connect.Request[appv1.ListScheduledTaskLogsRequest]) (*connect.Response[appv1.ListScheduledTaskLogsResponse], error) {
	return s.backend.ListScheduledTaskLogs(ctx, req)
}

func (s *Service) ClearScheduledTaskLogs(ctx context.Context, req *connect.Request[appv1.ClearScheduledTaskLogsRequest]) (*connect.Response[appv1.ClearScheduledTaskLogsResponse], error) {
	started := time.Now()
	response, err := s.backend.ClearScheduledTaskLogs(ctx, req)
	logging.Complete(ctx, "scheduler", "clear_logs", "scheduled task logs cleared", started, err)
	return response, err
}

func (s *Service) NextScheduledTaskRuns(ctx context.Context, req *connect.Request[appv1.NextScheduledTaskRunsRequest]) (*connect.Response[appv1.NextScheduledTaskRunsResponse], error) {
	return s.backend.NextScheduledTaskRuns(ctx, req)
}
