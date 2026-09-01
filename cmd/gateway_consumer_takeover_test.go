package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type consumerTakeoverStore struct{ getErr error }

func (s consumerTakeoverStore) Activate(context.Context, store.ChannelAdminTakeover) (*store.ChannelAdminTakeover, error) {
	return nil, errors.New("not implemented")
}

func (s consumerTakeoverStore) GetActive(context.Context, string, string, time.Time) (*store.ChannelAdminTakeover, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return &store.ChannelAdminTakeover{}, nil
}

func (s consumerTakeoverStore) ListActive(context.Context, store.ChannelAdminTakeoverListOptions) ([]store.ChannelAdminTakeover, int, error) {
	return nil, 0, errors.New("not implemented")
}

func (s consumerTakeoverStore) Release(context.Context, uuid.UUID, string, string, time.Time) (*store.ChannelAdminTakeover, error) {
	return nil, errors.New("not implemented")
}

func TestSuppressInboundForAdminTakeoverBeforeScheduler(t *testing.T) {
	msg := bus.InboundMessage{
		Channel: "cloudmini-net-page", ChatID: "customer-1",
		Metadata: map[string]string{"fb_mode": "messenger"},
	}
	ctx := store.WithTenantID(context.Background(), uuid.Must(uuid.NewV7()))

	if !suppressInboundForAdminTakeover(ctx, msg, &ConsumerDeps{AdminTakeovers: consumerTakeoverStore{}}) {
		t.Fatal("active takeover did not suppress pre-scheduler inbound")
	}
	if suppressInboundForAdminTakeover(ctx, msg, &ConsumerDeps{AdminTakeovers: consumerTakeoverStore{getErr: store.ErrChannelAdminTakeoverNotFound}}) {
		t.Fatal("missing takeover suppressed inbound")
	}
	if !suppressInboundForAdminTakeover(ctx, msg, &ConsumerDeps{AdminTakeovers: consumerTakeoverStore{getErr: errors.New("database unavailable")}}) {
		t.Fatal("takeover store failure must fail closed")
	}
	msg.Metadata["fb_mode"] = "comment"
	if suppressInboundForAdminTakeover(ctx, msg, &ConsumerDeps{AdminTakeovers: consumerTakeoverStore{}}) {
		t.Fatal("Facebook comment was incorrectly subject to Messenger takeover")
	}
}

var _ store.ChannelAdminTakeoverStore = consumerTakeoverStore{}
