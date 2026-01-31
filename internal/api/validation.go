package api

import (
	"fmt"
	"net/url"
	"time"

	"github.com/robfig/cron/v3"
)

func validateCreateJob(req CreateJobRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}

	if req.CronExpression == "" {
		return fmt.Errorf("cron_expression is required")
	}

	if err := validateCron(req.CronExpression); err != nil {
		return fmt.Errorf("invalid cron_expression: %w", err)
	}

	tz := req.Timezone
	if tz == "" {
		tz = "UTC"
	}
	if err := validateTimezone(tz); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}

	if req.WebhookURL == "" {
		return fmt.Errorf("webhook_url is required")
	}
	if err := validateWebhookURL(req.WebhookURL); err != nil {
		return fmt.Errorf("invalid webhook_url: %w", err)
	}

	return nil
}

func validateCron(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err
}

func validateTimezone(tz string) error {
	_, err := time.LoadLocation(tz)
	return err
}

func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}
