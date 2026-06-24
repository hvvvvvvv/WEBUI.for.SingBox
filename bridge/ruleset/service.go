package ruleset

import (
	"context"

	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Backend interface {
	ListRuleSets(context.Context, *connect.Request[appv1.ListRuleSetsRequest]) (*connect.Response[appv1.ListRuleSetsResponse], error)
	SaveRuleSets(context.Context, *connect.Request[appv1.SaveRuleSetsRequest]) (*connect.Response[appv1.SaveRuleSetsResponse], error)
	UpsertRuleSet(context.Context, *connect.Request[appv1.UpsertRuleSetRequest]) (*connect.Response[appv1.UpsertRuleSetResponse], error)
	DeleteRuleSet(context.Context, *connect.Request[appv1.DeleteRuleSetRequest]) (*connect.Response[appv1.DeleteRuleSetResponse], error)
	UpdateRuleSet(context.Context, *connect.Request[appv1.UpdateRuleSetRequest]) (*connect.Response[appv1.UpdateRuleSetResponse], error)
	UpdateAllRuleSets(context.Context, *connect.Request[appv1.UpdateAllRuleSetsRequest]) (*connect.Response[appv1.UpdateAllRuleSetsResponse], error)
	UpdateRuleSetHub(context.Context, *connect.Request[appv1.UpdateRuleSetHubRequest]) (*connect.Response[appv1.UpdateRuleSetHubResponse], error)
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

func (s *Service) SaveRuleSets(ctx context.Context, req *connect.Request[appv1.SaveRuleSetsRequest]) (*connect.Response[appv1.SaveRuleSetsResponse], error) {
	return s.backend.SaveRuleSets(ctx, req)
}

func (s *Service) UpsertRuleSet(ctx context.Context, req *connect.Request[appv1.UpsertRuleSetRequest]) (*connect.Response[appv1.UpsertRuleSetResponse], error) {
	return s.backend.UpsertRuleSet(ctx, req)
}

func (s *Service) DeleteRuleSet(ctx context.Context, req *connect.Request[appv1.DeleteRuleSetRequest]) (*connect.Response[appv1.DeleteRuleSetResponse], error) {
	return s.backend.DeleteRuleSet(ctx, req)
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

func (s *Service) GetRuleSetContent(ctx context.Context, req *connect.Request[appv1.GetRuleSetContentRequest]) (*connect.Response[appv1.GetRuleSetContentResponse], error) {
	return s.backend.GetRuleSetContent(ctx, req)
}

func (s *Service) SaveRuleSetContent(ctx context.Context, req *connect.Request[appv1.SaveRuleSetContentRequest]) (*connect.Response[appv1.SaveRuleSetContentResponse], error) {
	return s.backend.SaveRuleSetContent(ctx, req)
}

func (s *Service) ClearRuleSetContent(ctx context.Context, req *connect.Request[appv1.ClearRuleSetContentRequest]) (*connect.Response[appv1.ClearRuleSetContentResponse], error) {
	return s.backend.ClearRuleSetContent(ctx, req)
}
