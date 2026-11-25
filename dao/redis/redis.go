package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

var (
	rdb *redis.Client // Redis 客户端，不是单个连接，而是内部维护连接池，并发安全
)

// Init 按配置创建共享的 Redis 客户端，并通过 Ping 检查连通性。
func Init() (err error) {
	rdb = redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%d",
			viper.GetString("redis.host"),
			viper.GetInt("redis.port"),
		),
		Password: viper.GetString("redis.password"),
		DB:       viper.GetInt("redis.db"),
		PoolSize: viper.GetInt("redis.pool_size"),
	})

	// 为 Ping 创建带 5 秒截止时间的上下文，套接字读写也受客户端超时配置约束。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 注意：NewClient 只是创建客户端对象，并不会真正建立连接。
	// 真正的连接是在第一次执行命令（包括 Ping）时才懒加载建立的。
	// 因此必须 Ping 一次来验证网络、密码、DB 是否正确。
	// 不 Ping 就直接用，错误会被延迟到第一次业务命令时才暴露。
	_, err = rdb.Ping(ctx).Result()
	return
}


// Close 在 Init 成功后用于关闭共享的 Redis 客户端及其连接池。
func Close() {
	_ = rdb.Close()
}
