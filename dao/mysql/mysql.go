// Package mysql 创建并探测应用共享的 MySQL 连接池。
package mysql

import (
	// context 为启动探测提供取消信号和截止时间。
	"context"
	// database/sql 提供通用连接池 API，由 MySQL 驱动处理具体协议。
	"database/sql"
	// errors.Join 合并连接探测和清理过程中出现的错误。
	"errors"
	// fmt 用于包装打开及探测失败的阶段信息。
	"fmt"
	// net.JoinHostPort 将主机名或 IP 和端口组成正确的网络地址。
	"net"
	// strconv.Itoa 将整数端口转换成地址拼接需要的十进制字符串。
	"strconv"
	// time 用于指定建连时间、探测时长和 Go 端解析日期所用的时区。
	"time"

	// 入口传入已读取和校验的 MySQLConfig。
	"study/settings"

	// 导入驱动会向 database/sql 注册 mysql 名称。别名 mysqlDriver 用来和当前包区分。
	mysqlDriver "github.com/go-sql-driver/mysql"
)

// Open 创建连接池并完成启动探测。成功后由调用方负责关闭。
// *sql.DB 管理多条连接，可以安全地被多个请求共享。
func Open(ctx context.Context, cfg settings.MySQLConfig) (*sql.DB, error) {
	// 由驱动构造 DSN，统一编码库名和参数。JoinHostPort 同时支持 IPv4 和 IPv6。
	// 先创建驱动的默认配置对象。DSN 是后续连接时交给驱动的参数集合。
	dsn := mysqlDriver.NewConfig()
	// 设置认证用的 MySQL 用户名。
	dsn.User = cfg.User
	// 设置该用户的认证密码。连接串随后会包含密码，不应写入普通日志。
	dsn.Passwd = cfg.Password
	// 指定通过 TCP 建连，使 Addr 被解释为主机和端口。
	dsn.Net = "tcp"
	// 整数端口先转成字符串。对于 IPv6 地址，JoinHostPort 会添加需要的方括号。
	dsn.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	// 指定建立连接后默认选用的数据库。
	dsn.DBName = cfg.DBName
	// 使用 utf8mb4 连接字符集，以传输包括四字节字符在内的 Unicode 文本。
	dsn.Params = map[string]string{"charset": "utf8mb4"}
	// 启用日期和时间解析，使支持的日期类型可以读取为 time.Time。
	dsn.ParseTime = true
	// 将解析出的时间放在应用机器的本地时区。
	dsn.Loc = time.Local // 只影响 Go 端时间解析，MySQL 会话时区由服务端配置。
	// 三秒限制单次 TCP 拨号。下面的探测上下文另管整次启动检查。
	dsn.Timeout = 3 * time.Second

	// FormatDSN 按驱动规则生成连接串，sql.Open 据此创建连接池句柄。
	pool, err := sql.Open("mysql", dsn.FormatDSN())
	// 驱动收到无效参数等问题时，这一步可能失败。
	if err != nil {
		// 返回 nil 池和包含阶段的错误，调用方无需登记资源清理。
		return nil, fmt.Errorf("打开 MySQL 连接池: %w", err)
	}
	// 限制池中同时打开的连接总数，0 表示不设上限。
	pool.SetMaxOpenConns(cfg.MaxOpenConns)
	// 限制空闲连接保留数量，便于在复用连接和服务端资源之间取舍。
	pool.SetMaxIdleConns(cfg.MaxIdleConns)

	// sql.Open 创建池，PingContext 实际验证地址、凭据和数据库是否可用。
	// 从启动上下文派生五秒探测期限。父上下文提前取消时，探测也应停止。
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	// 退出 Open 时释放探测计时器。该 context 仅用于这次 Ping。
	defer cancel()
	// PingContext 会实际尝试连接并验证数据库是否可用。if 里的 err 是本次探测的结果。
	if err := pool.PingContext(probeCtx); err != nil {
		// 失败时先关闭刚创建的池，再把探测和关闭错误合起来返回。
		return nil, fmt.Errorf("探测 MySQL 连接: %w", errors.Join(err, pool.Close()))
	}
	// 成功后把池的所有权交给调用方，由它负责在整个服务退出时调用 Close。
	return pool, nil
}
