// 测试与应用入口使用同一个包，因此可以直接调用未导出的 serve。
package main

import (
	// context 用于人工触发停止信号，并在请求提前取消时释放测试处理器。
	"context"
	// io.ReadAll 用于读取完整响应体。
	"io"
	// net 创建回环监听器并尝试连接，以确认服务何时停止接收新连接。
	"net"
	// net/http 提供测试用服务器、客户端和状态码。
	"net/http"
	// testing 提供测试入口、失败报告和中止操作。
	"testing"
	// time 用于给测试等待设置上限，防止测试挂起。
	"time"

	// 复用项目真实路由来检查在途请求最终仍能返回根路径响应。
	"study/routes"

	// zap.NewNop 创建无输出日志器，测试不需要写日志文件。
	"go.uber.org/zap"
)

// TestServeGracefulShutdown 验证停止监听后仍会等待处理中请求完成。
func TestServeGracefulShutdown(t *testing.T) {
	// 处理器进入后关闭该通道，通知测试主 goroutine 可以触发停止。
	handlerStarted := make(chan struct{})
	// 测试关闭此通道后，处理器才继续执行，从而制造一个可控制的在途请求。
	releaseHandler := make(chan struct{})
	// 创建真正的 Gin 路由，但把日志输出交给无操作日志器。
	router := routes.Setup(zap.NewNop())
	// 构造只用于本测试的服务器，把等待逻辑放在真实路由之前。
	srv := &http.Server{
		// 适配成 http.Handler。w 用来写响应，r 携带请求及其上下文。
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 通过关闭通道通知测试，处理器已进入，当前请求确实在途。
			close(handlerStarted)
			// 阻塞到测试允许处理，或者客户端已经取消请求。
			select {
			// 测试确认监听已关闭后，会释放这个通道。
			case <-releaseHandler:
				// 把当前请求继续交给真实路由，以便返回 HTTP 200 和 ok。
				router.ServeHTTP(w, r)
			// 如果客户端提前取消，直接结束测试处理器，避免它一直等待。
			case <-r.Context().Done():
			}
		}),
	}
	// 即使测试提前失败，仍强制关闭服务器以释放活跃连接。
	defer srv.Close()

	// 只监听本机回环，端口 0 交给系统分配，避免和已有服务争抢固定端口。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	// 无法绑定本地端口时测试前提不成立。
	if err != nil {
		// 报告系统返回的错误，并立即结束本测试。
		t.Fatal(err)
	}
	// 登记监听器清理，覆盖尚未进入 Serve 就失败的情况。
	defer listener.Close()

	// 使用可手动取消的上下文模拟应用收到停止信号。
	stopCtx, stop := context.WithCancel(context.Background())
	// 测试提前返回时也取消该上下文，以便服务 goroutine 可以退出。
	defer stop()
	// 容量为 1，让后台服务可以报告一次结果，即使测试尚未开始读取。
	serveDone := make(chan error, 1)
	// 在独立 goroutine 启动真实的 serve，避免阻塞后面的请求和关机检查。
	go func() { serveDone <- serve(stopCtx, srv, listener) }()

	// 限制客户端请求时长，避免服务行为异常让测试无限等待。
	client := &http.Client{Timeout: 3 * time.Second}
	// 用独立缓冲通道传递成功收到的 HTTP 响应。
	responseDone := make(chan *http.Response, 1)
	// 用另一个缓冲通道传递客户端请求错误。
	requestErr := make(chan error, 1)
	// 后台请求会等待处理器释放，测试主 goroutine 要同时执行关机。
	go func() {
		// 使用系统实际分配的端口发送请求，覆盖真实网络读写。
		response, err := client.Get("http://" + listener.Addr().String() + "/")
		// 连接失败或客户端超时时，走错误分支。
		if err != nil {
			// 让执行断言的测试 goroutine 收到请求错误。
			requestErr <- err
			// 请求失败后结束后台函数，此时没有有效响应可以发送。
			return
		}
		// 请求成功后把响应交给测试主 goroutine 检查并关闭响应体。
		responseDone <- response
	}()

	// 必须先确认请求进入处理器，才能验证这次关机确实遇到了在途请求。
	select {
	case <-handlerStarted:
	// 如果请求先报错，则关机测试的前提未满足。
	case err := <-requestErr:
		t.Fatal(err)
	// 如果没有响应或错误，最多等待三秒。
	case <-time.After(3 * time.Second):
		t.Fatal("请求未进入处理器")
	}
	// 请求已进入处理器，主动取消上下文，让 serve 开始优雅关机。
	stop()

	// Shutdown 关闭监听器后，尝试新的连接应失败。原请求应仍在等待。
	// 为检查监听状态设两秒总期限。
	deadline := time.Now().Add(2 * time.Second)
	// 等待 Shutdown 在后台关闭监听器，期间定期尝试创建新连接。
	for {
		// 只探测 TCP 建连，单次尝试至多等待 100 毫秒。
		conn, err := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond)
		// 如果连接失败，停止尝试，继续检查原请求是否仍在等待。
		if err != nil {
			break
		}
		// 关闭探测成功的 TCP 连接，避免探测本身留下资源。
		conn.Close()
		// 如果超过期限还能连上，说明关机未及时停止监听。
		if time.Now().After(deadline) {
			t.Fatal("收到停止信号后仍接受新连接")
		}
		// 在两次尝试间短暂停顿，避免对端口做忙轮询。
		time.Sleep(10 * time.Millisecond)
	}
	// 以非阻塞方式检查 serve 是否错误地提前返回。
	select {
	// 原请求还被拦住时，收到任何退出结果都违反优雅关机的预期。
	case err := <-serveDone:
		t.Fatalf("原请求尚未释放，服务已退出: %v", err)
	// 没有退出结果时立即继续，说明服务仍在等待。
	default:
	}
	// 允许处理器进入真实 Gin 路由，原请求现在应正常结束。
	close(releaseHandler)

	// 等待原请求结果。成功、失败和超时分别由不同分支处理。
	select {
	case response := <-responseDone:
		// 读完响应后释放客户端响应体。
		defer response.Body.Close()
		// 读取完整正文，后续同时核对读错误、状态码和正文。
		body, err := io.ReadAll(response.Body)
		// 即使客户端成功收到响应，也要验证关机没有改变原路由的结果。
		if err != nil || response.StatusCode != http.StatusOK || string(body) != "ok" {
			t.Fatalf("原请求应收到 200 ok，实际状态为 %d，响应为 %q，错误为 %v", response.StatusCode, body, err)
		}
	// 如果收到通信错误，说明原请求未能正常完成。
	case err := <-requestErr:
		t.Fatal(err)
	// 如果处理器释放后长期没有结果，判定测试失败。
	case <-time.After(3 * time.Second):
		t.Fatal("原请求未完成")
	}

	// 原请求完成后等待服务真正退出，确认 Shutdown 没有卡住。
	select {
	case err := <-serveDone:
		// 正常关机应以 nil 结束，其他错误需要作为测试失败报告。
		if err != nil {
			t.Fatal(err)
		}
	// 给服务退出设置上限，避免关机回归让测试挂起。
	case <-time.After(3 * time.Second):
		t.Fatal("服务未完成关机")
	}
}
