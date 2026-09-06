package subscription

import (
	"context"
	"time"

	"guiforcores/bridge/logging"
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
	started := time.Now()
	response, err := s.backend.CreateSubscription(ctx, req)
	logging.Complete(ctx, "subscription", "create", "subscription created", started, err, "payload_bytes", len(req.Msg.GetSubscriptionJson()))
	return response, err
}

func (s *Service) UpdateSubscriptionConfig(ctx context.Context, req *connect.Request[appv1.UpdateSubscriptionConfigRequest]) (*connect.Response[appv1.UpdateSubscriptionConfigResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateSubscriptionConfig(ctx, req)
	logging.Complete(ctx, "subscription", "update_config", "subscription configuration updated", started, err, "payload_bytes", len(req.Msg.GetSubscriptionJson()))
	return response, err
}

func (s *Service) DeleteSubscription(ctx context.Context, req *connect.Request[appv1.DeleteSubscriptionRequest]) (*connect.Response[appv1.DeleteSubscriptionResponse], error) {
	started := time.Now()
	response, err := s.backend.DeleteSubscription(ctx, req)
	logging.Complete(ctx, "subscription", "delete", "subscription deleted", started, err, "subscription_id", req.Msg.GetId())
	return response, err
}

func (s *Service) ReorderSubscriptions(ctx context.Context, req *connect.Request[appv1.ReorderSubscriptionsRequest]) (*connect.Response[appv1.ReorderSubscriptionsResponse], error) {
	started := time.Now()
	response, err := s.backend.ReorderSubscriptions(ctx, req)
	logging.Complete(ctx, "subscription", "reorder", "subscriptions reordered", started, err, "total", len(req.Msg.GetIds()))
	return response, err
}

func (s *Service) UpdateSubscription(ctx context.Context, req *connect.Request[appv1.UpdateSubscriptionRequest]) (*connect.Response[appv1.UpdateSubscriptionResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateSubscription(ctx, req)
	total := 0
	var results []*appv1.TaskResult
	if response != nil {
		results = response.Msg.GetResults()
		total = len(results)
	}
	successes, failures := subscriptionResultCounts(results)
	attrs := []any{"subscription_id", req.Msg.GetId(), "total", total, "success_count", successes, "failure_count", failures}
	if err == nil && failures > 0 {
		logging.Partial(ctx, "subscription", "refresh", "subscription refresh completed with failures", started, attrs...)
	} else {
		logging.Complete(ctx, "subscription", "refresh", "subscription refreshed", started, err, attrs...)
	}
	return response, err
}

func (s *Service) UpdateAllSubscriptions(ctx context.Context, req *connect.Request[appv1.UpdateAllSubscriptionsRequest]) (*connect.Response[appv1.UpdateAllSubscriptionsResponse], error) {
	started := time.Now()
	response, err := s.backend.UpdateAllSubscriptions(ctx, req)
	total := 0
	var results []*appv1.TaskResult
	if response != nil {
		results = response.Msg.GetResults()
		total = len(results)
	}
	successes, failures := subscriptionResultCounts(results)
	attrs := []any{"total", total, "success_count", successes, "failure_count", failures}
	if err == nil && failures > 0 {
		logging.Partial(ctx, "subscription", "refresh_all", "subscription refresh completed with failures", started, attrs...)
	} else {
		logging.Complete(ctx, "subscription", "refresh_all", "subscriptions refreshed", started, err, attrs...)
	}
	return response, err
}

func subscriptionResultCounts(results []*appv1.TaskResult) (int, int) {
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

func (s *Service) GetSubscriptionContent(ctx context.Context, req *connect.Request[appv1.GetSubscriptionContentRequest]) (*connect.Response[appv1.GetSubscriptionContentResponse], error) {
	return s.backend.GetSubscriptionContent(ctx, req)
}

func (s *Service) SaveSubscriptionContent(ctx context.Context, req *connect.Request[appv1.SaveSubscriptionContentRequest]) (*connect.Response[appv1.SaveSubscriptionContentResponse], error) {
	started := time.Now()
	response, err := s.backend.SaveSubscriptionContent(ctx, req)
	logging.Complete(ctx, "subscription", "save_content", "subscription content saved", started, err, "subscription_id", req.Msg.GetId(), "content_bytes", len(req.Msg.GetContent()), "proxy_count", len(req.Msg.GetProxyIds()))
	return response, err
}
