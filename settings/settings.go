package settings

import (
	"fmt"

	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
)

// Init 从当前工作目录下的 conf 目录加载配置，并监听文件变化。
func Init() (err error) {
	//指定配置文件名称
	viper.SetConfigName("config")
	//指定配置文件类型
	viper.SetConfigType("yaml")
	//指定查找配置文件的路径(相对路径)
	viper.AddConfigPath(".")

	//读取配置信息
	err = viper.ReadInConfig()
	if err != nil {
		//读取配置信息失败
		fmt.Printf(
			"viper.ReadInConfig() failed:%v\n", err)
		return
	}

	// Viper 检测到文件变化时会重新读取配置，回调只负责打印提示。
	// 已经初始化的监听端口、连接池和日志器不会随配置重新创建。
	viper.WatchConfig()
	viper.OnConfigChange(func(in fsnotify.Event) {
		fmt.Println("配置文件修改了")
	})
	return
}

// GinLogger 使用全局 Zap 日志器记录 HTTP 请求。
func GinLogger() gin.HandlerFunc {
	return ginzap.Ginzap(zap.L(), time.RFC3339, true)
}

// GinRecovery 恢复请求处理中的 panic，并记录错误。
func GinRecovery(stack bool) gin.HandlerFunc {
	return ginzap.RecoveryWithZap(zap.L(), stack)
}
