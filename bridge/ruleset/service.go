package ruleset

import (
	"context"
	"time"

	"guiforcores/bridge/logging"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Backend interface {
	ListRuleSets(context.Context, *connect.Request[appv1.ListRuleSetsRequest]) (*connect.Response[appv1.ListRuleSetsResponse], error)
	CreateRuleSet(context.Context, *connect.Request[appv1.CreateRuleSetRequest]) (*connect.Response[appv1.CreateRuleSetResponse], error)
	UpdateRuleSetConfig(context.Context, *connect.Request[appv1.UpdateRuleSetConfigRequest]) (*connect.Response[appv1.UpdateRuleSetConfigResponse], error)
	DeleteRuleSet(context.Context, *connect.Request[appv1.DeleteRuleSetRequest]) (*connect.Response[appv1.DeleteRuleSetResponse], error)
	ReorderRuleSets(context.Context, *connect.Request[appv1.ReorderRuleSetsRequest]) (*connect.Response[appv1.ReorderRuleSetsResponse], error)
	UpdateRuleSet(context.Context, *connect.Request[appv1.UpdateRuleSetRequest]) (*connect.Response[appv1.UpdateRuleSetResponse], error)
	UpdateAllRuleSets(context.Context, *connect.Request[appv1.UpdateAllRuleSetsRequest]) (*connect.Response[appv1.UpdateAllRuleSetsResponse], error)
	UpdateRuleSetHub(context.Context, *connect.Request[appv1.UpdateRuleSetHubRequest]) (*connect.Response[appv1.UpdateRuleSetHubResponse], error)
	PreviewRuleSetHub(context.Context, *connect.Request[appv1.PreviewRuleSetHubRequest]) (*connect.Response[appv1.PreviewRuleSetHubResponse], error)
	GetRuleSetContent(context.Context, *connect.Request[appv1.GetRuleSetContentRequest]) (*connect.Response[appv1.GetRuleSetContentResponse], error)
	SaveRuleSetContent(context.Context, *connect.Request[appv1.SaveRuleSetContentRequest]) (*connect.Response[appv1.SaveRuleSetContentResponse], error)
	ClearRuleSetContent(context.Context, *connect.Request[appv1.ClearRuleSetContentRequest]) (*connect.Response[appv1.ClearRuleSetContentResponse], error)
}

type Service struct {
	backend Backend
}

func NewService(backend Backend) *Service {
	return &Service{backend: backend}
}

func (s *Service) ListRuleSets(ctx context.Context, req *connect.Request[appv1.ListRuleSetsRequest]) (*connect.Response[appv1.ListRuleSetsResponse], error) {
	return s.backend.ListRuleSets(ctx, req)
}

func (s *Service) CreateRuleSet(ctx context.Context, req *connect.Request[appv1.CreateRuleSetRequest]) (*connect.Response[appv1.CreateRuleSetResponse], error) {
	started := time.Now()
	response, err := s.backend.CreateRuleSet(ctx, req)
	logging.Complete(ctx, "ruleset", "create", "rule set created", started, err, "payload_bytes", len(req.Msg.GetRulesetJson()))
	return response, err
}

func (s *Service) UpdateRuleSetConfig(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetConfigRequest]) (*connect.Response[appv1.UpdateRuleSetConfigResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateRuleSetConfig(ctx, req)
	logging.Complete(ctx, "ruleset", "update_config", "rule set configuration updated", started, err, "payload_bytes", len(req.Msg.GetRulesetJson()))
	return response, err
}

func (s *Service) DeleteRuleSet(ctx context.Context, req *connect.Request[appv1.DeleteRuleSetRequest]) (*connect.Response[appv1.DeleteRuleSetResponse], error) {
	started := time.Now()
	response, err := s.backend.DeleteRuleSet(ctx, req)
	logging.Complete(ctx, "ruleset", "delete", "rule set deleted", started, err, "ruleset_id", req.Msg.GetId())
	return response, err
}

func (s *Service) ReorderRuleSets(ctx context.Context, req *connect.Request[appv1.ReorderRuleSetsRequest]) (*connect.Response[appv1.ReorderRuleSetsResponse], error) {
	started := time.Now()
	response, err := s.backend.ReorderRuleSets(ctx, req)
	logging.Complete(ctx, "ruleset", "reorder", "rule sets reordered", started, err, "total", len(req.Msg.GetIds()))
	return response, err
}

func (s *Service) UpdateRuleSet(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetRequest]) (*connect.Response[appv1.UpdateRuleSetResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateRuleSet(ctx, req)
	total := 0
	var results []*appv1.TaskResult
	if response != nil {
		results = response.Msg.GetResults()
		total = len(results)
	}
	successes, failures := ruleSetResultCounts(results)
	attrs := []any{"ruleset_id", req.Msg.GetId(), "total", total, "success_count", successes, "failure_count", failures}
	if err == nil && failures > 0 {
		logging.Partial(ctx, "ruleset", "refresh", "rule set refresh completed with failures", started, attrs...)
	} else {
		logging.Complete(ctx, "ruleset", "refresh", "rule set refreshed", started, err, attrs...)
	}
	return response, err
}

func (s *Service) UpdateAllRuleSets(ctx context.Context, req *connect.Request[appv1.UpdateAllRuleSetsRequest]) (*connect.Response[appv1.UpdateAllRuleSetsResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateAllRuleSets(ctx, req)
	total := 0
	var results []*appv1.TaskResult
	if response != nil {
		results = response.Msg.GetResults()
		total = len(results)
	}
	successes, failures := ruleSetResultCounts(results)
	attrs := []any{"total", total, "success_count", successes, "failure_count", failures}
	if err == nil && failures > 0 {
		logging.Partial(ctx, "ruleset", "refresh_all", "rule set refresh completed with failures", started, attrs...)
	} else {
		logging.Complete(ctx, "ruleset", "refresh_all", "rule sets refreshed", started, err, attrs...)
	}
	return response, err
}

func ruleSetResultCounts(results []*appv1.TaskResult) (int, int) {
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

func (s *Service) UpdateRuleSetHub(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetHubRequest]) (*connect.Response[appv1.UpdateRuleSetHubResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateRuleSetHub(ctx, req)
	logging.Complete(ctx, "ruleset", "update_hub", "rule set hub updated", started, err)
	return response, err
}

func (s *Service) PreviewRuleSetHub(ctx context.Context, req *connect.Request[appv1.PreviewRuleSetHubRequest]) (*connect.Response[appv1.PreviewRuleSetHubResponse], error) {
	return s.backend.PreviewRuleSetHub(ctx, req)
}

func (s *Service) GetRuleSetContent(ctx context.Context, req *connect.Request[appv1.GetRuleSetContentRequest]) (*connect.Response[appv1.GetRuleSetContentResponse], error) {
	return s.backend.GetRuleSetContent(ctx, req)
}

func (s *Service) SaveRuleSetContent(ctx context.Context, req *connect.Request[appv1.SaveRuleSetContentRequest]) (*connect.Response[appv1.SaveRuleSetContentResponse], error) {
	started := time.Now()
	response, err := s.backend.SaveRuleSetContent(ctx, req)
	logging.Complete(ctx, "ruleset", "save_content", "rule set content saved", started, err, "ruleset_id", req.Msg.GetId(), "content_bytes", len(req.Msg.GetContent()))
	return response, err
}

func (s *Service) ClearRuleSetContent(ctx context.Context, req *connect.Request[appv1.ClearRuleSetContentRequest]) (*connect.Response[appv1.ClearRuleSetContentResponse], error) {
	started := time.Now()
	response, err := s.backend.ClearRuleSetContent(ctx, req)
	logging.Complete(ctx, "ruleset", "clear_content", "rule set content cleared", started, err, "ruleset_id", req.Msg.GetId())
	return response, err
}
