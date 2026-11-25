// 主包生成可执行程序，Go 运行时从此包的 main 函数进入。
package main

import (
	// context 用于传播停止信号，并为优雅关机设置独立期限。
	"context"
	// errors 用于识别被包装的错误，以及合并运行错误和资源关闭错误。
	"errors"
	// flag 注册并解析 -config 命令行参数。
	"flag"
	// fmt 用于构造监听地址、包装错误并打印启动失败原因。
	"fmt"
	// net 创建 TCP 监听器。绑定端口的结果可以同步检查。
	"net"
	// net/http 提供 HTTP 服务器、生命周期方法及标准关机错误。
	"net/http"
	// os 提供标准错误流、系统中断信号和进程退出接口。
	"os"
	// os/signal 将操作系统发送的信号转换成可等待的上下文取消。
	"os/signal"
	// syscall 提供用于请求进程结束的 SIGTERM 信号常量。
	"syscall"
	// time 用于表达 HTTP 读取、空闲和关机时长。
	"time"

	// 下面五个包属于 study 模块。入口组合它们并负责管理资源，具体实现留在各模块。
	"study/dao/mysql"
	"study/dao/redis"
	"study/logger"
	"study/routes"
	"study/settings"

	// Gin 提供运行模式，Zap 提供写入日志的结构化字段。
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// main 处理启动参数和进程退出码。run 返回时已完成资源清理。
func main() {
	// 注册 -config 参数。String 返回字符串指针，默认值相对于进程工作目录。
	configPath := flag.String("config", "config.yaml", "YAML 配置文件路径")
	// 解析命令行后，configPath 指向的值才会反映用户传入的路径。
	flag.Parse()
	// 解引用取得实际路径并运行服务。run 返回前会执行它登记的 defer。
	if err := run(*configPath); err != nil {
		// 输出到 stderr。配置或日志初始化失败时，这个输出渠道仍可用。
		fmt.Fprintln(os.Stderr, err)
		// 用非零退出码告诉终端或服务管理器本次运行失败。os.Exit 会直接结束进程。
		os.Exit(1)
	}
}

// run 装配配置、日志、数据库和 HTTP 服务，并管理它们的生命周期。
func run(configPath string) (runErr error) {
	// 在连接依赖前订阅信号，启动过程中也可以取消数据库探测。
	// 无父级截止时间的 Background 是信号上下文的起点。操作系统中断或 SIGTERM 都会取消 stopCtx。
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// run 退出时解除信号订阅并释放上下文关联资源，即使后续配置加载失败也会执行。
	defer stop()

	// 调用 settings.Load 读取 configPath 指向的 YAML 及同目录可选的 .env。cfg 保存校验后的结构体，err 保存加载失败原因。
	cfg, err := settings.Load(configPath)
	// err 为 nil 表示配置可用。读取失败、解码失败或校验未通过时走这个分支。
	if err != nil {
		// 把已经带有阶段信息的错误交给 main。后续日志器、数据库和 HTTP 服务都还未创建。
		return err
	}
	// 根据项目自定义的模式值配置 Gin。必须在创建路由引擎前完成映射。
	switch cfg.App.Mode {
	case "dev":
		// 开发模式允许 Gin 输出路由注册和调试信息。
		gin.SetMode(gin.DebugMode)
	case "test":
		// 测试模式减少 Gin 自身的调试输出。
		gin.SetMode(gin.TestMode)
	case "prod":
		// 生产模式使用 Gin 的 ReleaseMode，关闭开发模式提示。
		gin.SetMode(gin.ReleaseMode)
	default:
		// 拒绝未识别的模式。%q 会用引号标出原始值，空字符串也容易辨认。
		return fmt.Errorf("无效的 app.mode %q，应为 dev、test 或 prod", cfg.App.Mode)
	}

	// 只传日志配置。返回值依次是可复用的日志器、文件清理函数和初始化错误。
	log, closeLog, err := logger.New(cfg.Log)
	// 日志级别无效或文件无法写入时，结束启动流程。
	if err != nil {
		// %w 保留底层错误，外层补充出错阶段，便于定位和使用 errors.Is 检查。
		return fmt.Errorf("初始化日志: %w", err)
	}
	// 用命名返回值 runErr 接收最终结果。defer 在返回前合并关日志的错误，保留原有运行错误。
	defer func() { runErr = errors.Join(runErr, closeLog()) }()

	// main 持有依赖。之后添加业务路由时，可通过构造函数把池传给 DAO。
	// defer 逆序执行，先关闭数据库和缓存，再关闭日志文件。
	// 打开 MySQL 连接池并执行启动探测。父上下文被停止信号取消时，探测也能结束。
	mysqlPool, err := mysql.Open(stopCtx, cfg.MySQL)
	// Open 失败时已经负责清理它创建的池，此处无需再次关闭。
	if err != nil {
		// 附加初始化阶段并返回。日志清理此前已登记，仍会执行。
		return fmt.Errorf("初始化 MySQL: %w", err)
	}
	// 只有成功获取连接池后才登记清理。匿名函数在 run 返回时执行。
	defer func() {
		// Close 停止池接受新查询，并等待已经发起的查询完成后关闭连接。
		if err := mysqlPool.Close(); err != nil {
			// 清理失败时追加带 MySQL 阶段的错误，已有的启动或运行错误也继续保留。
			runErr = errors.Join(runErr, fmt.Errorf("关闭 MySQL: %w", err))
		}
	}()

	// 创建 Redis 客户端并验证 PING，同样传停止上下文和该模块自己的配置。
	redisClient, err := redis.Open(stopCtx, cfg.Redis)
	// Redis 初始化失败时，仍会执行前面已登记的 MySQL 和日志清理。
	if err != nil {
		// 用 %w 保留 Redis 底层错误，例如认证失败或连接被拒绝。
		return fmt.Errorf("初始化 Redis: %w", err)
	}
	// 在 MySQL 之后登记 Redis 清理，退出时 Redis 会先于 MySQL 关闭。
	defer func() {
		// 关闭客户端与它持有的连接池，检查关闭操作返回的错误。
		if err := redisClient.Close(); err != nil {
			// 给 Redis 清理错误补充阶段，并把它合入 run 的最终结果。
			runErr = errors.Join(runErr, fmt.Errorf("关闭 Redis: %w", err))
		}
	}()

	// 创建标准库 HTTP Server。结构体只保存设置，实际绑定端口在下面执行。
	srv := &http.Server{
		// ":端口" 没有指定 IP，因此监听可用的本地网络地址。
		Addr: fmt.Sprintf(":%d", cfg.App.Port),
		// Gin 引擎实现 http.Handler。传入已有日志器后，请求日志和应用日志共用输出规则。
		Handler: routes.Setup(log),
		// 限制读取完整请求头的时间，避免客户端长期占着连接只发送少量头部数据。
		ReadHeaderTimeout: 5 * time.Second,
		// 限制 keep-alive 连接在两次请求之间可以空闲多久。
		IdleTimeout: 60 * time.Second,
	}

	// 先同步绑定端口，确保启动日志与实际监听状态一致。
	// 按配置绑定 TCP 地址。成功返回可用于接收连接的 listener。
	listener, err := net.Listen("tcp", srv.Addr)
	// 端口已被占用或地址不可用时，Listen 返回错误。
	if err != nil {
		// 记录尝试绑定的地址并保留底层错误。已创建的依赖会通过 defer 释放。
		return fmt.Errorf("监听 HTTP 地址 %s: %w", srv.Addr, err)
	}
	// 登记额外的监听器清理。Serve 或 Shutdown 已关闭它时，这里重复关闭的错误不会影响返回结果。
	defer listener.Close()
	// 端口已经绑定才写启动日志，并把应用名和实际监听地址作为独立 JSON 字段。
	log.Info("HTTP 服务开始监听", zap.String("app", cfg.App.Name), zap.String("addr", listener.Addr().String()))

	// 进入 HTTP 生命周期，直到收到停止信号或 Serve 返回才继续。
	if err := serve(stopCtx, srv, listener); err != nil {
		// 监听或关机失败时直接交给 main 处理，run 中的 defer 仍会执行。
		return err
	}
	// serve 无错误返回后记录关机完成，此时日志句柄仍由 run 持有。
	log.Info("HTTP 服务已关闭")
	// 表示服务正常结束。若后续 defer 发现清理错误，它仍可写入命名返回值。
	return nil
}

// serve 接收监听错误或停止信号。停止时先等待请求完成，再把控制权交回 run 清理依赖。
func serve(stopCtx context.Context, srv *http.Server, listener net.Listener) error {
	// Serve 在独立 goroutine 接收请求。缓冲通道允许它先退出再由调用方收取结果。
	// 缓冲大小为 1，只需要传递一次 Serve 的最终结果。
	serveErr := make(chan error, 1)
	// Serve 会阻塞等待请求，因此放入独立 goroutine，当前 goroutine 可以同时等待停止信号。
	go func() {
		// 在已有监听器上开始处理请求，结束时把 Serve 返回的错误发送给等待方。
		serveErr <- srv.Serve(listener)
	}()

	// select 同时等待服务自身退出和外部停止信号。任一事件到达即可继续。
	select {
	// 这一分支处理 Serve 在收到停止通知前已经返回的情况。
	case err := <-serveErr:
		// errors.Is 能识别经过包装的 ErrServerClosed。它是关闭服务器的正常返回值。
		if !errors.Is(err, http.ErrServerClosed) {
			// 其他错误属于运行故障，附加阶段后向上传递。
			return fmt.Errorf("运行 HTTP 服务: %w", err)
		}
		// 正常关闭不需要再进入下面的 Shutdown 流程。
		return nil
	// 接收到停止信号后，该上下文的 Done 通道关闭。跳出 select 进入关机流程。
	case <-stopCtx.Done():
	}

	// 停止信号对应的上下文已经取消，给正在处理的请求单独留出五秒。
	// 用仍有效的 Background 派生新的五秒上下文。若从 stopCtx 派生，关机会立即收到取消。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	// 无论关机成功还是失败，都释放这次超时上下文的计时器。
	defer cancel()
	// Shutdown 关闭监听器和空闲连接，再等待活跃请求结束，最多等待到 ctx 的截止时间。
	if err := srv.Shutdown(ctx); err != nil {
		// 优雅关机失败后用 Close 关闭剩余连接，并把两个操作可能产生的错误一起返回。
		return fmt.Errorf("关闭 HTTP 服务: %w", errors.Join(err, srv.Close()))
	}

	// 即使停止信号与监听故障同时发生，也保留 Serve 的真实错误。
	// Shutdown 完成后读取 goroutine 的结果，同时确认 Serve 已经返回。
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		// 遇到真实监听故障时仍然返回错误，避免它被恰好到来的停止信号掩盖。
		return fmt.Errorf("运行 HTTP 服务: %w", err)
	}
	// 活跃请求已结束，Serve 也以正常关闭的状态返回。
	return nil
}
