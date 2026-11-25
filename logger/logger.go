package logger

import (
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/natefinch/lumberjack"
)

//接收 viper 里的 4 个日志配置，返回一个 zapcore.WriteSyncer（zap 要求的写入目标类型）
func getLogWriter(filename string, maxSize, maxBackups, maxAge int) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   false,
	}
	return zapcore.AddSync(lumberJackLogger)
}

// initLogger 初始化自定义 zap 日志器
func Init() (err error) {
	// 1. 编码器（决定日志格式）：基于生产配置微调，把时间和级别改成人类易读格式
	encoderConfig := zap.NewProductionEncoderConfig()

	// 时间格式：2026-11-23T22:45:00+0800
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// 级别大写：DEBUG / INFO / WARN / ERROR
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	// NewJSONEncoder 输出 JSON（适合线上检索）；本地开发想看文本可换成 NewConsoleEncoder
	encoder := zapcore.NewJSONEncoder(encoderConfig)

	// 写入目标（决定日志写到哪）
	writeSyncer := getLogWriter(
		viper.GetString("log.filename"),
		viper.GetInt("log.max_size"),
		viper.GetInt("log.max_backups"),
		viper.GetInt("log.max_age"),
	)

	//把配置里的字符串日志级别转成 zap 能用的级别对象
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(viper.GetString("log.level"))); err != nil {
		level = zapcore.InfoLevel
	}

	// 3. 组装 Core = 编码器 + 写入目标 + 最低日志级别
	// 级别从低到高：Debug < Info < Warn < Error < DPanic < Panic < Fatal
	core := zapcore.NewCore(encoder, writeSyncer, level)

	// 4. 生成 Logger
	// zap.AddCaller() 会自动在日志里记录调用的文件名和行号（如 main.go:68），方便排查
	lg := zap.New(core, zap.AddCaller())

	//替换zap库中全局logger
	zap.ReplaceGlobals(lg)
	return nil
}

// Sync 导出一个同步函数，由 main 在程序退出前调用
func Sync() {
	_ = zap.L().Sync() // stdout 的 invalid argument 错误可安全忽略
}
