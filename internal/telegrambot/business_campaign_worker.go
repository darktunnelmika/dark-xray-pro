package telegrambot

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

type campaignTask struct {
	Delivery      delivery
	ID, AttemptID string
	LeaseUntil    int64
}
type campaignBackend interface {
	ClaimCampaign(context.Context, int64) (campaignTask, bool, error)
	FinishCampaign(context.Context, campaignTask, DeliveryOutcome, error) error
}
type maintenanceBackend interface {
	Maintain(context.Context, int64) error
}

func (b *BusinessBackend) ClaimCampaign(ctx context.Context, botID int64) (campaignTask, bool, error) {
	if !b.CampaignsEnabled {
		return campaignTask{}, false, nil
	}
	c, ok, err := b.store.TelegramClaimCampaignDeliveryV1(ctx, botID, time.Now())
	if err != nil || !ok {
		return campaignTask{}, ok, err
	}
	task := campaignTask{ID: c.DeliveryID, AttemptID: c.AttemptID, LeaseUntil: c.LeaseUntilUnix, Delivery: delivery{ID: c.DeliveryID, BotID: c.BotID, ChatID: c.ChatID, Kind: "campaign_message"}}
	markup := &wireKeyboard{}
	for _, row := range c.Content.Buttons {
		buttons := []wireButton{}
		for _, button := range row {
			buttons = append(buttons, wireButton{Text: button.Text, Style: button.Style, URL: button.URL})
		}
		markup.Rows = append(markup.Rows, buttons)
	}
	if len(markup.Rows) == 0 {
		markup = nil
	}
	if c.Content.PhotoFileID != "" {
		task.Delivery.Kind = "campaign_photo"
		task.Delivery.Payload, _ = json.Marshal(wirePhoto{ChatID: c.ChatID, Photo: c.Content.PhotoFileID, Caption: c.Content.Text, Markup: markup})
	} else {
		m := wireMessage{ChatID: c.ChatID, Text: c.Content.Text, Markup: markup}
		m.Preview.Disabled = true
		task.Delivery.Payload, _ = json.Marshal(m)
	}
	return task, true, nil
}

func (b *BusinessBackend) FinishCampaign(ctx context.Context, task campaignTask, out DeliveryOutcome, sendErr error) error {
	r := business.TelegramCampaignDeliveryResultV1{DeliveryID: task.ID, AttemptID: task.AttemptID, Outcome: "unknown"}
	var failure *APIError
	if out.State == "sent" && out.MessageID > 0 {
		r.Outcome = "sent"
		r.MessageID = out.MessageID
	} else if errors.As(sendErr, &failure) {
		if failure.Definitive && failure.Code == 429 && failure.RetryAfter > 0 && !failure.Fatal {
			r.Outcome = "rate_limited"
			r.ErrorCode = 429
			r.RetryAfterSeconds = int64(failure.RetryAfter / time.Second)
		} else if failure.Definitive && failure.Code >= 400 && failure.Code < 500 && failure.Code != 429 {
			r.Outcome = "permanent_failure"
			r.ErrorCode = failure.Code
		}
	}
	return b.store.TelegramFinishCampaignDeliveryV1(ctx, task.Delivery.BotID, r, time.Now())
}

func (b *BusinessBackend) Maintain(ctx context.Context, botID int64) error {
	if !b.RewardsEnabled {
		return nil
	}
	return b.store.TelegramSettleDueRewardsV1(ctx, botID, 25)
}
