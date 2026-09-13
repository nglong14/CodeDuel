package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

var errInvalidRedisURL = errors.New("parse REDIS_URL: invalid redis connection URL")

func RedisOptions(url, addr string) (*redis.Options, error) {
	if url != "" {
		opts, err := redis.ParseURL(url)
		if err != nil {
			return nil, errInvalidRedisURL
		}
		opts.ContextTimeoutEnabled = true
		return opts, nil
	}
	return &redis.Options{
		Addr:                  addr,
		ContextTimeoutEnabled: true,
	}, nil
}

func RedisHost(url, addr string) string {
	if url == "" {
		return addr
	}
	if opts, err := redis.ParseURL(url); err == nil {
		return opts.Addr
	}
	return ""
}

func NewRedis(ctx context.Context, url, addr string) (*redis.Client, error) {
	opts, err := RedisOptions(url, addr)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}
