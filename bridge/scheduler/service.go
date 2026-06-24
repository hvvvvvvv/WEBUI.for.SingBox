package scheduler

import (
	"context"

	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Backend interface {
	StartScheduler()
	StopScheduler()
	ListScheduledTasks(context.Context, *connect.Request[appv1.ListScheduledTasksRequest]) (*connect.Response[appv1.ListScheduledTasksResponse], error)
	SaveScheduledTasks(context.Context, *connect.Request[appv1.SaveScheduledTasksRequest]) (*connect.Response[appv1.SaveScheduledTasksResponse], error)
	UpsertScheduledTask(context.Context, *connect.Request[appv1.UpsertScheduledTaskRequest]) (*connect.Response[appv1.UpsertScheduledTaskResponse], error)
	DeleteScheduledTask(context.Context, *connect.Request[appv1.DeleteScheduledTaskRequest]) (*connect.Response[appv1.DeleteScheduledTaskResponse], error)
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
}

func (s *Service) Stop() {
	s.backend.StopScheduler()
}

func (s *Service) ListScheduledTasks(ctx context.Context, req *connect.Request[appv1.ListScheduledTasksRequest]) (*connect.Response[appv1.ListScheduledTasksResponse], error) {
	return s.backend.ListScheduledTasks(ctx, req)
}

func (s *Service) SaveScheduledTasks(ctx context.Context, req *connect.Request[appv1.SaveScheduledTasksRequest]) (*connect.Response[appv1.SaveScheduledTasksResponse], error) {
	return s.backend.SaveScheduledTasks(ctx, req)
}

func (s *Service) UpsertScheduledTask(ctx context.Context, req *connect.Request[appv1.UpsertScheduledTaskRequest]) (*connect.Response[appv1.UpsertScheduledTaskResponse], error) {
	return s.backend.UpsertScheduledTask(ctx, req)
}

func (s *Service) DeleteScheduledTask(ctx context.Context, req *connect.Request[appv1.DeleteScheduledTaskRequest]) (*connect.Response[appv1.DeleteScheduledTaskResponse], error) {
	return s.backend.DeleteScheduledTask(ctx, req)
}

func (s *Service) RunScheduledTask(ctx context.Context, req *connect.Request[appv1.RunScheduledTaskRequest]) (*connect.Response[appv1.RunScheduledTaskResponse], error) {
	return s.backend.RunScheduledTask(ctx, req)
}

func (s *Service) ListScheduledTaskLogs(ctx context.Context, req *connect.Request[appv1.ListScheduledTaskLogsRequest]) (*connect.Response[appv1.ListScheduledTaskLogsResponse], error) {
	return s.backend.ListScheduledTaskLogs(ctx, req)
}

func (s *Service) ClearScheduledTaskLogs(ctx context.Context, req *connect.Request[appv1.ClearScheduledTaskLogsRequest]) (*connect.Response[appv1.ClearScheduledTaskLogsResponse], error) {
	return s.backend.ClearScheduledTaskLogs(ctx, req)
}

func (s *Service) NextScheduledTaskRuns(ctx context.Context, req *connect.Request[appv1.NextScheduledTaskRunsRequest]) (*connect.Response[appv1.NextScheduledTaskRunsResponse], error) {
	return s.backend.NextScheduledTaskRuns(ctx, req)
}
