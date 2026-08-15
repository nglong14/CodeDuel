package gateway

import (
	"context"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func userChannel(userID uuid.UUID) string {
	return "codeduel:user:" + userID.String()
}

func subscribeUser(ctx context.Context, rdb *redis.Client, userID uuid.UUID, c *conn) func() {
	sub := rdb.Subscribe(ctx, userChannel(userID))
	go fanout(sub.Channel(), c)
	return func() {
		_ = sub.Close()
	}
}

func fanout(ch <-chan *redis.Message, c *conn) {
	for msg := range ch {
		c.Send([]byte(msg.Payload))
	}
}
