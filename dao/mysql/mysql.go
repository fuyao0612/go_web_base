package mysql

import (
	"database/sql"
	"fmt"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// 全局数据库对象：整个程序共享一个 *sql.DB。
// 注意：*sql.DB 不是"一个连接"，而是"连接池管理器"，
// 它内部维护着多个连接，并发安全，可以被多个 goroutine 同时使用。
var db *sql.DB

// Init 按配置创建共享的 MySQL 连接池，验证数据库连通性并设置连接数上限。
func Init() (err error) {
	// DSN（Data Source Name）：数据库连接串，格式：
	//   用户名:密码@tcp(主机地址:端口号)/数据库名?参数
	// 参数说明：
	//   charset=utf8mb4  -> 使用 utf8mb4 字符集，支持 emoji 和 4 字节字符
	//   parseTime=True   -> 把 MySQL 的 DATE/DATETIME/TIMESTAMP 字段自动扫描成 Go 的 time.Time，
	//                       否则会扫描成 []byte，Scan 到 time.Time 时会报错
	//   loc=Local        -> 使用应用进程的本地时区解析 time.Time，不会修改数据库会话时区
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		viper.GetString("mysql.user"),
		viper.GetString("mysql.password"),
		viper.GetString("mysql.host"),
		viper.GetInt("mysql.port"),
		viper.GetString("mysql.dbname"),
	)

	// sql.Open 只是"校验驱动名和 DSN 格式"，并不会真正建立连接。
	// 真正的连接是在第一次真正执行 SQL / Ping 时才懒加载建立的。
	// 所以这里返回 err 基本只会在 DSN 格式有误或驱动名写错时出现。
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		zap.L().Error("connect DB failed", zap.Error(err))
		return
	}

	// Ping 会真正向数据库发起一次连接测试，确认网络、账号、密码、库名是否正确。
	// 如果这里不 Ping，错误会被延迟到后面第一次查询时才暴露，定位更困难。
	err = db.Ping()
	if err != nil {
		fmt.Println(err)
		return err
	}
	fmt.Println("good!") // 打印成功标识，证明连接可用

	// 最大打开连接数（连接池最多能同时持有的连接数，含正在使用+空闲）。
	// 设置过大会打爆数据库，设置过小会造成高并发时排队等待。
	db.SetMaxOpenConns(viper.GetInt("mysql.max_open_conns"))

	// 最大空闲连接数（连接池空闲时最多保留几个连接）。
	// 超过这个数量的空闲连接会被关闭，避免资源浪费。
	// 一般 <= MaxOpenConns。
	db.SetMaxIdleConns(viper.GetInt("mysql.max_idle_conns"))
	return
}

// Close 在 Init 成功后用于关闭共享的 MySQL 连接池。
func Close() {
	_ = db.Close()
}
