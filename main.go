package main

import (
	"fmt"
	"study/logger"
	"study/settings"

	"go.uber.org/zap"
)

func main() {
	//1.加载配置
	if err := settings.Init(); err != nil {
		fmt.Printf("settings.Init error%v\n", err)
	}
	//2.初始化日志
	if err := logger.Init(); err != nil {
		fmt.Printf("logger.Init error%v\n", err)
	}
	zap.L().Debug("logger init good !!!")
	defer logger.Sync()

	//3.初始化mysql\redis连接
	if err := mysql.Init(); err != nil {
		fmt.Printf("mysql.Init error%v\n", err)
	}

	if err := redis.Init(); err != nil {
		fmt.Printf("redis.Init error%v\n", err)
	}
	
	//4.注册路由
	//5.启动服务
}
