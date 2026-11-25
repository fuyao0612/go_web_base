package logger

import (
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/natefinch/lumberjack"
)

// getLogWriter 创建支持轮转的日志文件写入器，并适配为 Zap 的写入接口。
// maxSize 的单位为 MB，maxAge 的单位为天，maxBackups 是保留的备份文件数。
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

// Init 根据 Viper 配置创建 JSON 文件日志器，并替换 Zap 的全局日志器。
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

// Sync 在退出前调用全局日志器的同步方法，并忽略它返回的错误。
func Sync() {
	_ = zap.L().Sync()
}
