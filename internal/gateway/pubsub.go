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

func subscribeUser(ctx context.Context, rdb *redis.Client, userID uuid.UUID, c *conn) (func(), error) {
	sub := rdb.Subscribe(ctx, userChannel(userID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("subscribe user channel: %w", err)
	}
	go fanout(sub.Channel(), c)
	return func() {
		_ = sub.Close()
	}, nil
}

func fanout(ch <-chan *redis.Message, c *conn) {
	for msg := range ch {
		c.Send([]byte(msg.Payload))
	}
}
