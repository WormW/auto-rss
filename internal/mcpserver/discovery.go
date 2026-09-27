package mcpserver

import (
	"context"
	"fmt"

	"github.com/WormW/auto-rss/internal/service/subscriptiondiscovery"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) prepareSubscription(ctx context.Context, req *mcp.CallToolRequest, input subscriptiondiscovery.PrepareInput) (*mcp.CallToolResult, subscriptiondiscovery.Draft, error) {
	if s.discovery == nil {
		return nil, subscriptiondiscovery.Draft{}, fmt.Errorf("discovery unavailable")
	}
	draft, err := s.discovery.Prepare(ctx, input)
	if err != nil {
		return nil, subscriptiondiscovery.Draft{}, err
	}
	return resultWithText(draft)
}

func (s *Server) confirmSubscription(ctx context.Context, req *mcp.CallToolRequest, input subscriptiondiscovery.ConfirmInput) (*mcp.CallToolResult, CreateSubscriptionOutput, error) {
	if s.discovery == nil {
		return nil, CreateSubscriptionOutput{}, fmt.Errorf("discovery unavailable")
	}
	sub, err := s.discovery.Confirm(ctx, input)
	if err != nil {
		return nil, CreateSubscriptionOutput{}, err
	}
	return resultWithText(CreateSubscriptionOutput{Subscription: summarizeSubscription(*sub)})
}
