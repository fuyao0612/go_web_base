package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"study/dao/mysql"
	"study/dao/redis"
	"study/logger"
	"study/routes"
	"study/settings"
	"syscall"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// main 按依赖顺序初始化组件，启动 HTTP 服务并处理退出信号。
func main() {
	//1.加载配置
	if err := settings.Init(); err != nil {
		fmt.Printf("settings.Init error%v\n", err)
		return
	}
	
	//2.初始化日志
	if err := logger.Init(); err != nil {
		fmt.Printf("logger.Init error%v\n", err)
		return
	}
	zap.L().Debug("logger init good !!!")
	// defer 按逆序执行。正常退出时先关闭 Redis 和 MySQL，再同步日志。
	defer logger.Sync()

	//3.初始化mysql\redis连接
	if err := mysql.Init(); err != nil {
		fmt.Printf("mysql.Init error%v\n", err)
		return
	}
	defer mysql.Close()

	if err := redis.Init(); err != nil {
		fmt.Printf("redis.Init error%v\n", err)
		return
	}
	defer redis.Close()

	//4.注册路由
	r := routes.Setup()
	// 5. 使用配置中的端口启动 Gin HTTP 服务。

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", viper.GetInt("app.port")),
		Handler: r,
	}

	// ListenAndServe 会阻塞，将它放在独立 goroutine 中以便主 goroutine 等待退出信号。
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen%s\n", err)
		}
	}()

	//等待中断信号来优雅关闭服务器.为关闭服务器设置一个5s的超时
	quit := make(chan os.Signal, 1) //创建一个接收信号的通道
	//kill默认发送syscall.SIGTERM信号
	//kill -2 发送syscall.SIGINT信号(ctrl+c)
	//kill -9 发送syscall.SIGKILL信号,但是不能被捕获,所以不需要添加

	//signal.Notify把收到的syscall.SIGINT or syscall.SIGTERM信号发给quit
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit //阻塞,当接收到上述信号才会往下执行
	zap.L().Info("shutdown server...")

	//创建一个5秒超时的context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Shutdown 停止接受新连接，并等待正在处理的请求结束。超过期限时返回错误。
	if err := srv.Shutdown(ctx); err != nil {
		zap.L().Fatal("server shutdown:", zap.Error(err))
	}
	zap.L().Info("server exiting!!!")

}
