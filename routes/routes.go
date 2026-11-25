package routes

import (
	"net/http"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Setup 创建 Gin 引擎，安装全局日志和 panic 恢复中间件，并注册路由。
func Setup() *gin.Engine {
	// gin.New 创建空引擎，由下面的 Use 调用显式安装中间件。
	r := gin.New()

	lg:=zap.L()

	r.Use(
		ginzap.Ginzap(lg, time.RFC3339, true),
		ginzap.RecoveryWithZap(lg, true),
	)
	// 根路径返回固定文本，用于确认 HTTP 路由可达。
	r.GET("/",func(c *gin.Context){
		c.String(http.StatusOK,"ok")
	})
	return r
}