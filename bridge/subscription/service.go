package subscription

import (
	"context"

	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Backend interface {
	ListSubscriptions(context.Context, *connect.Request[appv1.ListSubscriptionsRequest]) (*connect.Response[appv1.ListSubscriptionsResponse], error)
	CreateSubscription(context.Context, *connect.Request[appv1.CreateSubscriptionRequest]) (*connect.Response[appv1.CreateSubscriptionResponse], error)
	UpdateSubscriptionConfig(context.Context, *connect.Request[appv1.UpdateSubscriptionConfigRequest]) (*connect.Response[appv1.UpdateSubscriptionConfigResponse], error)
	DeleteSubscription(context.Context, *connect.Request[appv1.DeleteSubscriptionRequest]) (*connect.Response[appv1.DeleteSubscriptionResponse], error)
	ReorderSubscriptions(context.Context, *connect.Request[appv1.ReorderSubscriptionsRequest]) (*connect.Response[appv1.ReorderSubscriptionsResponse], error)
	UpdateSubscription(context.Context, *connect.Request[appv1.UpdateSubscriptionRequest]) (*connect.Response[appv1.UpdateSubscriptionResponse], error)
	UpdateAllSubscriptions(context.Context, *connect.Request[appv1.UpdateAllSubscriptionsRequest]) (*connect.Response[appv1.UpdateAllSubscriptionsResponse], error)
	GetSubscriptionContent(context.Context, *connect.Request[appv1.GetSubscriptionContentRequest]) (*connect.Response[appv1.GetSubscriptionContentResponse], error)
	SaveSubscriptionContent(context.Context, *connect.Request[appv1.SaveSubscriptionContentRequest]) (*connect.Response[appv1.SaveSubscriptionContentResponse], error)
}

type Service struct {
	backend Backend
}

func NewService(backend Backend) *Service {
	return &Service{backend: backend}
}

func (s *Service) ListSubscriptions(ctx context.Context, req *connect.Request[appv1.ListSubscriptionsRequest]) (*connect.Response[appv1.ListSubscriptionsResponse], error) {
	return s.backend.ListSubscriptions(ctx, req)
}

func (s *Service) CreateSubscription(ctx context.Context, req *connect.Request[appv1.CreateSubscriptionRequest]) (*connect.Response[appv1.CreateSubscriptionResponse], error) {
	return s.backend.CreateSubscription(ctx, req)
}

func (s *Service) UpdateSubscriptionConfig(ctx context.Context, req *connect.Request[appv1.UpdateSubscriptionConfigRequest]) (*connect.Response[appv1.UpdateSubscriptionConfigResponse], error) {
	return s.backend.UpdateSubscriptionConfig(ctx, req)
}

func (s *Service) DeleteSubscription(ctx context.Context, req *connect.Request[appv1.DeleteSubscriptionRequest]) (*connect.Response[appv1.DeleteSubscriptionResponse], error) {
	return s.backend.DeleteSubscription(ctx, req)
}

func (s *Service) ReorderSubscriptions(ctx context.Context, req *connect.Request[appv1.ReorderSubscriptionsRequest]) (*connect.Response[appv1.ReorderSubscriptionsResponse], error) {
	return s.backend.ReorderSubscriptions(ctx, req)
}

func (s *Service) UpdateSubscription(ctx context.Context, req *connect.Request[appv1.UpdateSubscriptionRequest]) (*connect.Response[appv1.UpdateSubscriptionResponse], error) {
	return s.backend.UpdateSubscription(ctx, req)
}

func (s *Service) UpdateAllSubscriptions(ctx context.Context, req *connect.Request[appv1.UpdateAllSubscriptionsRequest]) (*connect.Response[appv1.UpdateAllSubscriptionsResponse], error) {
	return s.backend.UpdateAllSubscriptions(ctx, req)
}

func (s *Service) GetSubscriptionContent(ctx context.Context, req *connect.Request[appv1.GetSubscriptionContentRequest]) (*connect.Response[appv1.GetSubscriptionContentResponse], error) {
	return s.backend.GetSubscriptionContent(ctx, req)
}

func (s *Service) SaveSubscriptionContent(ctx context.Context, req *connect.Request[appv1.SaveSubscriptionContentRequest]) (*connect.Response[appv1.SaveSubscriptionContentResponse], error) {
	return s.backend.SaveSubscriptionContent(ctx, req)
}
