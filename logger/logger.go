// Package logger 负责创建应用和 Gin 中间件共用的 Zap 日志器。
package logger

import (
	// errors.Is 和 errors.Join 分别用于识别文件不存在及合并日志清理错误。
	"errors"
	// fmt 用于给日志参数、文件和输出错误添加阶段信息。
	"fmt"
	// os 用于创建目录、识别路径类型并验证文件可写。
	"os"
	// filepath.Dir 取得日志文件的父目录，兼容当前系统的路径格式。
	"path/filepath"
	// strings.TrimSpace 帮助识别用户只填写空格的日志级别。
	"strings"

	// 日志配置是普通结构体，由入口传入，不需要在这里重新读取 YAML。
	"study/settings"

	// Zap 提供日志 API，zapcore 组合编码格式、输出目标和过滤级别。
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	// Lumberjack 在当前日志文件达到上限时轮转并管理历史文件。
	"gopkg.in/natefinch/lumberjack.v2"
)

// New 创建 JSON 文件日志器，并返回由调用方在退出时执行的清理函数。
func New(cfg settings.LogConfig) (*zap.Logger, func() error, error) {
	// 先声明 Zap 认识的级别变量，后面用配置字符串填充。
	var level zapcore.Level
	// 显式拒绝空配置，避免空值被解析为默认 Info 级别。
	if strings.TrimSpace(cfg.Level) == "" {
		// 创建失败时日志器和清理函数都返回 nil，第三个返回值说明原因。
		return nil, nil, fmt.Errorf("log.level 不能为空")
	}
	// 将配置字符串转换成字节切片后交给 UnmarshalText。成功时它更新 level。
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		// 返回用户填写的级别并保留 Zap 的解析错误，方便判断是哪个配置出错。
		return nil, nil, fmt.Errorf("无效的 log.level %q: %w", cfg.Level, err)
	}

	// 生产编码配置提供 JSON 字段名。这里单独调整时间和级别的表示方式。
	// 取 Zap 的生产编码默认字段名和其他编码选项作为基础。
	encoderCfg := zap.NewProductionEncoderConfig()
	// 把日志自身的时间戳改为包含时区的 ISO 8601 形式，便于人工查看。
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	// 将级别写成 DEBUG、INFO、WARN 等大写文本。
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder

	// 创建 Lumberjack 日志器前先检查目标文件，权限和路径问题会在服务启动前暴露。
	if err := prepareLogFile(cfg.Filename); err != nil {
		// 带上配置路径和底层错误返回。此时还没有可供调用方使用的日志器。
		return nil, nil, fmt.Errorf("准备日志文件 %q: %w", cfg.Filename, err)
	}

	// Lumberjack 在写入时打开日志文件，并根据大小、天数和数量轮转。
	// 配置轮转写入器。它会在实际写日志时打开并持有文件句柄。
	writer := &lumberjack.Logger{
		// 指定当前正在写入的日志文件。
		Filename: cfg.Filename,
		// 设置当前文件的轮转阈值，单位为 MB。
		MaxSize: cfg.MaxSize,
		// 按天限制历史日志保留时间。
		MaxAge: cfg.MaxAge,
		// 限制可保留的历史日志文件数量。
		MaxBackups: cfg.MaxBackups,
	}
	// NewJSONEncoder 生成 JSON 编码器。AddSync 把 writer 适配为输出接口，level 决定哪些条目能进入输出。
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), zapcore.AddSync(writer), level)
	// 根据 Core 构造日志器，AddCaller 将日志调用所在的文件和行号写入日志。
	log := zap.New(core, zap.AddCaller())

	// Sync 同步 Zap 的输出。日志文件句柄由 Lumberjack 持有，需要单独关闭。
	// 返回的闭包捕获上面创建的日志器和写入器，使调用方退出时能释放同一套资源。
	closeLog := func() error {
		// 先调用 Zap 的同步接口，再关闭 Lumberjack 写入器。两步都执行，Join 收集两步的错误。
		if err := errors.Join(log.Sync(), writer.Close()); err != nil {
			// 如果清理有任何错误，包装成日志关闭错误供入口合并。
			return fmt.Errorf("关闭日志输出: %w", err)
		}
		// 两个清理动作都成功时返回 nil。
		return nil
	}
	// 把日志器与它对应的清理函数一起交给入口，第三个返回值为 nil 表示创建成功。
	return log, closeLog, nil
}

// prepareLogFile 提前验证路径可写，避免启动后才从日志器的内部错误看到失败。
func prepareLogFile(path string) error {
	// 递归创建日志父目录。目录已存在时可继续，遇到文件占位或权限不足时会失败。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		// 把目录错误交给 New，在外层补上目标日志路径。
		return err
	}
	// 读取文件元信息，用于判断文件是否存在以及它是不是普通文件。
	info, err := os.Stat(path)
	// 不存在可以继续走创建流程。其他 Stat 错误会妨碍判断路径是否可以安全使用。
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// 把原始文件系统错误交回给调用方包装。
		return err
	}
	// 只有 Stat 成功才使用 info。已有目标是目录或设备等非普通文件时拒绝启动。
	if err == nil && !info.Mode().IsRegular() {
		// 给出明确的路径类型错误，避免把目录当成日志文件。
		return fmt.Errorf("日志路径必须指向普通文件")
	}

	// 以追加模式检查文件，保留历史内容，把轮转留给 Lumberjack。
	// 以仅写、追加模式打开，缺失时创建。0o600 指定新文件的默认访问权限。
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	// 打开失败通常说明目标不可写，此处立即终止准备流程。
	if err != nil {
		// 返回底层打开错误，保留操作系统的失败原因。
		return err
	}
	// 验证通过后立即关闭临时句柄，实际写日志时由 Lumberjack 自行打开。
	return file.Close()
}
