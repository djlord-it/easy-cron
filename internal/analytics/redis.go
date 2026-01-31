package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/djlord-it/easy-cron/internal/domain"
)

type RedisSink struct {
	client *redis.Client
}

func NewRedisSink(client *redis.Client) *RedisSink {
	return &RedisSink{client: client}
}

func (s *RedisSink) Write(ctx context.Context, event domain.TriggerEvent, config domain.AnalyticsConfig) error {
	if !config.Enabled {
		return nil
	}

	key := buildKey(event.ProjectID.String(), event.JobID.String(), config.Type, event.ScheduledAt, config.Window)

	pipe := s.client.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, config.Retention)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline: %w", err)
	}

	return nil
}

func buildKey(projectID, jobID string, typ domain.AnalyticsType, t time.Time, window time.Duration) string {
	bucket := truncateToBucket(t, window)
	return fmt.Sprintf("p:%s:j:%s:%s:%s", projectID, jobID, typ, bucket)
}

func truncateToBucket(t time.Time, window time.Duration) string {
	t = t.UTC()
	switch window {
	case time.Minute:
		return t.Format("200601021504")
	case 5 * time.Minute:
		minute := (t.Minute() / 5) * 5
		return t.Format("2006010215") + fmt.Sprintf("%02d", minute)
	case time.Hour:
		return t.Format("2006010215")
	default:
		return t.Format("200601021504")
	}
}
