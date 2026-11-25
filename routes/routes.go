// Package routes 创建 Gin 引擎并集中注册 HTTP 路由。
package routes

import (
	// net/http 提供状态码常量，time 提供请求日志时间格式。
	"net/http"
	"time"

	// ginzap 将 Gin 中间件的输出接到 Zap。这里显式命名便于辨认调用来源。
	ginzap "github.com/gin-contrib/zap"
	// Gin 创建路由引擎并为每次请求提供上下文。
	"github.com/gin-gonic/gin"
	// Zap 日志器由入口传入，其输出配置对路由和应用日志统一生效。
	"go.uber.org/zap"
)

// Setup 使用调用方配置好的 Zap 日志器，创建并返回 HTTP 路由。
func Setup(log *zap.Logger) *gin.Engine {
	// gin.New 创建没有预装日志和恢复中间件的引擎，下面自行挂载所需中间件。
	router := gin.New()

	// 当前服务直接接收请求。部署在反向代理后，应配置实际可信的代理地址。
	// nil 让 Gin 使用连接的对端地址，避免错误信任客户端自带的转发头。
	// nil 表示不信任任何代理，当前参数无需解析 CIDR，因此这里忽略返回错误。
	_ = router.SetTrustedProxies(nil)

	// 请求日志包在恢复中间件外层，以便在恢复 panic 后记录最终响应状态。
	// RFC3339 和 UTC 参数用于请求附带的时间字段。
	// Use 将中间件挂到引擎，在注册的路由上按传入顺序执行。
	router.Use(
		// 第一个中间件记录请求结果。RFC3339 控制附加时间字段的格式，true 表示用 UTC。
		ginzap.Ginzap(log, time.RFC3339, true),
		// 第二个中间件捕获处理器的 panic 并记录错误。true 表示在恢复日志中附上堆栈。
		ginzap.RecoveryWithZap(log, true),
	)

	// 根路由只用于验证 HTTP 服务可以响应。
	// 注册 GET 和根路径 / 的组合。Gin 把当前请求的上下文传进匿名函数。
	router.GET("/", func(c *gin.Context) {
		// 通过上下文写 HTTP 200 和纯文本 ok。该响应不会执行 MySQL 或 Redis 查询。
		c.String(http.StatusOK, "ok")
	})
	// 把已经挂好中间件和路由的引擎交还入口，可直接赋给 http.Server.Handler。
	return router
}
