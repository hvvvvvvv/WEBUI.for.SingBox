package ruleset

import (
	"context"

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
	return s.backend.CreateRuleSet(ctx, req)
}

func (s *Service) UpdateRuleSetConfig(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetConfigRequest]) (*connect.Response[appv1.UpdateRuleSetConfigResponse], error) {
	return s.backend.UpdateRuleSetConfig(ctx, req)
}

func (s *Service) DeleteRuleSet(ctx context.Context, req *connect.Request[appv1.DeleteRuleSetRequest]) (*connect.Response[appv1.DeleteRuleSetResponse], error) {
	return s.backend.DeleteRuleSet(ctx, req)
}

func (s *Service) ReorderRuleSets(ctx context.Context, req *connect.Request[appv1.ReorderRuleSetsRequest]) (*connect.Response[appv1.ReorderRuleSetsResponse], error) {
	return s.backend.ReorderRuleSets(ctx, req)
}

func (s *Service) UpdateRuleSet(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetRequest]) (*connect.Response[appv1.UpdateRuleSetResponse], error) {
	return s.backend.UpdateRuleSet(ctx, req)
}

func (s *Service) UpdateAllRuleSets(ctx context.Context, req *connect.Request[appv1.UpdateAllRuleSetsRequest]) (*connect.Response[appv1.UpdateAllRuleSetsResponse], error) {
	return s.backend.UpdateAllRuleSets(ctx, req)
}

func (s *Service) UpdateRuleSetHub(ctx context.Context, req *connect.Request[appv1.UpdateRuleSetHubRequest]) (*connect.Response[appv1.UpdateRuleSetHubResponse], error) {
	return s.backend.UpdateRuleSetHub(ctx, req)
}

func (s *Service) PreviewRuleSetHub(ctx context.Context, req *connect.Request[appv1.PreviewRuleSetHubRequest]) (*connect.Response[appv1.PreviewRuleSetHubResponse], error) {
	return s.backend.PreviewRuleSetHub(ctx, req)
}

func (s *Service) GetRuleSetContent(ctx context.Context, req *connect.Request[appv1.GetRuleSetContentRequest]) (*connect.Response[appv1.GetRuleSetContentResponse], error) {
	return s.backend.GetRuleSetContent(ctx, req)
}

func (s *Service) SaveRuleSetContent(ctx context.Context, req *connect.Request[appv1.SaveRuleSetContentRequest]) (*connect.Response[appv1.SaveRuleSetContentResponse], error) {
	return s.backend.SaveRuleSetContent(ctx, req)
}

func (s *Service) ClearRuleSetContent(ctx context.Context, req *connect.Request[appv1.ClearRuleSetContentRequest]) (*connect.Response[appv1.ClearRuleSetContentResponse], error) {
	return s.backend.ClearRuleSetContent(ctx, req)
}
