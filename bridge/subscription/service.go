package subscription

import (
	"context"

	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
)

type Backend interface {
	ListSubscriptions(context.Context, *connect.Request[appv1.ListSubscriptionsRequest]) (*connect.Response[appv1.ListSubscriptionsResponse], error)
	SaveSubscriptions(context.Context, *connect.Request[appv1.SaveSubscriptionsRequest]) (*connect.Response[appv1.SaveSubscriptionsResponse], error)
	UpsertSubscription(context.Context, *connect.Request[appv1.UpsertSubscriptionRequest]) (*connect.Response[appv1.UpsertSubscriptionResponse], error)
	DeleteSubscription(context.Context, *connect.Request[appv1.DeleteSubscriptionRequest]) (*connect.Response[appv1.DeleteSubscriptionResponse], error)
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

func (s *Service) SaveSubscriptions(ctx context.Context, req *connect.Request[appv1.SaveSubscriptionsRequest]) (*connect.Response[appv1.SaveSubscriptionsResponse], error) {
	return s.backend.SaveSubscriptions(ctx, req)
}

func (s *Service) UpsertSubscription(ctx context.Context, req *connect.Request[appv1.UpsertSubscriptionRequest]) (*connect.Response[appv1.UpsertSubscriptionResponse], error) {
	return s.backend.UpsertSubscription(ctx, req)
}

func (s *Service) DeleteSubscription(ctx context.Context, req *connect.Request[appv1.DeleteSubscriptionRequest]) (*connect.Response[appv1.DeleteSubscriptionResponse], error) {
	return s.backend.DeleteSubscription(ctx, req)
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
