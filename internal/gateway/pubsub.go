package gateway

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

func userChannel(userID uuid.UUID) string {
	return redisx.UserChannel(userID)
}

func subscribeUser(ctx context.Context, rdb *redis.Client, userID uuid.UUID) (*redis.PubSub, error) {
	sub := rdb.Subscribe(ctx, userChannel(userID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("subscribe user channel: %w", err)
	}
	return sub, nil
}

func fanout(ch <-chan *redis.Message, c *conn) {
	for msg := range ch {
		c.deliveryMu.Lock()
		c.Send([]byte(msg.Payload))
		c.deliveryMu.Unlock()
	}
}
