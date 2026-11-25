// Package redis 创建并探测应用共享的 Redis 客户端。
package redis

import (
	// context 让 Redis 启动探测继承取消信号并设置截止时间。
	"context"
	// errors.Join 在返回探测失败的同时保留可能发生的关闭错误。
	"errors"
	// fmt 给错误添加 Redis 探测阶段，便于和 MySQL 错误区分。
	"fmt"
	// net 按 TCP 地址规则拼接主机和端口。
	"net"
	// strconv 把整数端口转换为十进制字符串。
	"strconv"
	// time 用于表达建连和探测的时限。
	"time"

	// settings 提供入口已加载的 RedisConfig。
	"study/settings"

	// go-redis 提供 Redis 客户端、选项和连接池实现。
	"github.com/redis/go-redis/v9"
)

// Open 创建并探测 Redis 客户端。成功后由调用方负责关闭。
// redis.Client 内部维护连接池，可供多个请求并发使用。
func Open(ctx context.Context, cfg settings.RedisConfig) (*redis.Client, error) {
	// NewClient 保存选项并创建客户端，后续命令会触发建立实际连接。
	client := redis.NewClient(&redis.Options{
		// 将主机和端口拼为 Redis 的 TCP 地址，兼容主机名、IPv4 和 IPv6。
		Addr: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		// 有密码时通过客户端在连接上认证，空值用于不需要密码的 Redis 实例。
		Password: cfg.Password,
		// 指定连接初始化时要使用的 Redis 逻辑数据库编号。
		DB: cfg.DB,
		// 配置池的基础大小。传 0 时由客户端决定默认值。
		PoolSize: cfg.PoolSize,
		// 限制每次拨号建立 TCP 连接的时间。
		DialTimeout: 3 * time.Second,
		// 开启后，命令的截止时间也能参与设置套接字读写期限。
		ContextTimeoutEnabled: true, // 套接字读写使用命令上下文的截止时间。
	})

	// NewClient 延迟建连。PING 会触发建连、认证和逻辑库选择。
	// 为启动探测派生五秒截止时间，并继承进程停止时的取消信号。
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	// 无论探测以哪种方式结束，都及时释放该上下文的计时器。
	defer cancel()
	// 发出 Redis PING。Err 取出命令执行错误，此处只需要确认探测是否成功。
	if err := client.Ping(probeCtx).Err(); err != nil {
		// 探测失败就关闭已创建客户端及其池，Join 同时保留探测和关闭错误。
		return nil, fmt.Errorf("探测 Redis 连接: %w", errors.Join(err, client.Close()))
	}
	// 探测成功才返回客户端。后续连接复用与退出清理由调用方负责。
	return client, nil
}
