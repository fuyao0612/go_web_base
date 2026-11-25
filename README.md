# Gin Web 通用脚手架

这个项目把 Gin 路由、Viper 配置、Zap 日志、MySQL 和 Redis 接到同一个 Go 应用中。服务启动后，访问 `GET /` 会收到 `200 OK` 和纯文本 `ok`。首次运行需要同时准备 MySQL 和 Redis。

本教程包含两条路径。手上已经有本仓库时，从「运行现有项目」开始。想理解每个组件如何接入时，从「从空目录搭建」开始。

## 运行现有项目

### 1. 准备环境

- 安装 Go，版本至少满足 [go.mod](go.mod) 的 `go` 指令。Go 命令可用 `go version` 检查。
- 准备可连接的 MySQL 和 Redis。本教程的容器命令使用 Docker Desktop 和 PowerShell。在 Windows 上可用 `docker version` 确认 Docker 客户端和服务端都可用。
- 在本项目的根目录打开终端。能看到 `main.go`、`go.mod` 和 `config.yaml` 即可。

~~~powershell
go version
go mod download
~~~

### 2. 准备 MySQL 和 Redis

已有服务时，直接使用自己的服务地址。MySQL 的数据库应提前创建，应用账号应能连接该数据库。Redis 的逻辑库应存在于该服务支持的范围内。

也可以在本机启动容器。下面的命令把容器端口映射到宿主机回环地址，方便本地开发。MySQL 使用宿主机的 `3307`，避免与本机常见的 `3306` 冲突。使用 PowerShell 7 的 `Read-Host -MaskInput` 输入两个不同的开发密码，并保留应用账号密码供下一步填写。

~~~powershell
$env:MYSQL_ROOT_PASSWORD = Read-Host -MaskInput '为容器 root 设置密码'
$env:MYSQL_PASSWORD = Read-Host -MaskInput '为应用账号 scaffold 设置密码'
docker run -d --name goweb-mysql -p 127.0.0.1:3307:3306 -e MYSQL_ROOT_PASSWORD -e MYSQL_PASSWORD -e MYSQL_DATABASE=cjh -e MYSQL_USER=scaffold mysql:8.4
Remove-Item Env:MYSQL_ROOT_PASSWORD,Env:MYSQL_PASSWORD

docker run -d --name goweb-redis -p 127.0.0.1:6379:6379 redis:8

docker logs --tail 30 goweb-mysql
docker exec goweb-redis redis-cli ping
~~~

首次启动 MySQL 需要初始化数据目录。等日志出现最终的 `ready for connections`，并显示端口 `3306`，再启动应用。Redis 的 `PING` 应返回 `PONG`。如果 `6379` 已占用，可以把宿主机映射改为 `6380:6379`，并同步修改 `redis.port`。已有同名容器时可用 `docker start goweb-mysql goweb-redis` 复用。

### 3. 填写 YAML 和密码

使用上面的容器时，在保留其余字段的同时，把 [config.yaml](config.yaml) 中 MySQL 的 `host`、`port`、`user`、`dbname` 分别设为 `127.0.0.1`、`3307`、`scaffold`、`cjh`。使用自有服务时填相应地址、端口和账号。Redis 示例容器使用 `127.0.0.1:6379`，逻辑库为 `0`。

~~~powershell
Copy-Item .env.example .env
~~~

在 `.env` 中把 `APP_MYSQL_PASSWORD` 填成应用账号的密码。示例 Redis 容器无需密码，`APP_REDIS_PASSWORD` 保持为空即可。含有空格或 `#` 的值可用 dotenv 支持的引号格式，例如 `APP_MYSQL_PASSWORD='你的本地密码'`。部署时也可以由服务管理器注入同名环境变量。

| 配置块 | 常用字段及含义 |
| --- | --- |
| `app` | `name` 会写入启动日志，`mode` 接受 `dev`、`test`、`prod`，`port` 是 HTTP 监听端口。 |
| `log` | `level` 是最低日志级别，`filename` 是日志路径，`max_size` 以 MB 计，`max_age` 以天计，`max_backups` 是历史文件数量上限。 |
| `mysql` | `host`、`port`、`user`、`dbname` 指定连接。`max_open_conns` 和 `max_idle_conns` 限制每个应用进程的连接池。 |
| `redis` | `host`、`port` 指定地址，`db` 指定逻辑库，`pool_size` 设置基础池大小，`0` 使用客户端默认值。 |

密码优先级为 **进程环境变量 → 同目录的 `.env` → YAML**。显式设置为空也会覆盖下一级来源。仓库里的 YAML 可保存公开的默认值，实际密码放在忽略的 `.env` 或由部署环境注入。此前进入 Git 的密码如果仍然有效，需要轮换。

程序默认从当前工作目录读取 `config.yaml`。可以通过 `-config` 指定其他 YAML，此时 `.env` 从该 YAML 的目录读取。相对日志路径仍以进程工作目录为准。配置在启动时读取，改完后重启生效。

### 4. 运行与验证

~~~powershell
go run .
~~~

另开一个终端：

~~~powershell
curl.exe -i http://127.0.0.1:8081/
Get-Content web_app.log -Tail 10
~~~

预期状态码为 `200 OK`，响应体为 `ok`。正常启动和请求日志会写入配置指定的 JSON 日志文件。运行窗口按 `Ctrl+C` 后，服务停止接受新连接，最多等待正在处理的请求五秒，然后释放依赖和日志文件。

需要生成可执行文件时，将其放进已忽略的 `dist`：

~~~powershell
New-Item -ItemType Directory -Force dist | Out-Null
go build -o dist/goweb.exe .
.\dist\goweb.exe -config .\config.yaml
~~~

## 从空目录搭建

### 1. 初始化 Go 模块和最小 HTTP 服务

~~~powershell
New-Item -ItemType Directory goweb-scaffold
Set-Location goweb-scaffold
go mod init study
go get github.com/gin-gonic/gin
~~~

本项目使用 `study` 作为模块名，因此源码里的内部导入从 `study/` 开始。用自己的模块名时，要同步替换所有内部包导入路径。

先在根目录创建最小的 `main.go`，只验证 Gin 安装和入口：

~~~go
package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	if err := router.Run(":8081"); err != nil {
		log.Fatal(err)
	}
}
~~~

运行 `go run .` 并请求 `http://127.0.0.1:8081/`。确认返回 `ok` 后停止进程，再逐步接入下面的模块。这个临时入口最后由完整的 [main.go](main.go) 替换。

### 2. 加入各组件依赖和目录

~~~powershell
go get github.com/spf13/viper github.com/joho/godotenv
go get go.uber.org/zap github.com/gin-contrib/zap gopkg.in/natefinch/lumberjack.v2
go get github.com/go-sql-driver/mysql github.com/redis/go-redis/v9
New-Item -ItemType Directory -Force settings,logger,dao/mysql,dao/redis,routes
~~~

`go get` 解析当时可用的版本。如果要复现本仓库使用的版本，使用本仓库的 `go.mod`、`go.sum`，或者按其中的直接依赖版本逐个执行 `go get 包名@版本`。

~~~text
.
├── main.go                   解析启动参数、装配依赖、管理 HTTP 生命周期
├── config.yaml               可共享的默认配置
├── .env.example              本地密码变量模板
├── settings/settings.go      Viper 加载、环境变量覆盖与校验
├── logger/logger.go          Zap JSON 日志和 Lumberjack 轮转
├── dao/mysql/mysql.go        MySQL 连接池和启动探测
├── dao/redis/redis.go        Redis 客户端和启动探测
├── routes/routes.go          Gin 路由、请求日志和 panic 恢复
├── main_test.go              HTTP 优雅退出回归测试
└── settings/settings_test.go 配置加载回归测试
~~~

### 3. 用结构体保存启动配置

在 `settings/settings.go` 中创建 `Config`，嵌套 `AppConfig`、`LogConfig`、`MySQLConfig` 和 `RedisConfig`。结构体字段必须可导出，YAML 键通过 `mapstructure` 标签映射。例如 `max_open_conns` 映射到 `MySQLConfig.MaxOpenConns`。

按以下顺序实现 `settings.Load(path)`：

1. 调用 `viper.New()` 创建独立实例，再通过 `SetConfigFile` 和 `ReadInConfig` 加载 YAML。
2. 读同目录的可选 `.env`。用 `os.LookupEnv` 判断进程环境变量是否已设置，按配置优先级覆盖两个密码键。
3. 用 `UnmarshalExact` 解码到 `Config`，未知字段直接报错。再校验端口、必填字段和连接池参数。
4. 把配置结构体返回给入口。各组件通过参数接收自己的配置，整个读取流程集中在 `settings` 包。

完整实现见 [settings/settings.go](settings/settings.go)。建立同结构的 [config.yaml](config.yaml)，并把 [.env.example](.env.example) 和 [.gitignore](.gitignore) 一起加入项目。

### 4. 创建日志器，让 HTTP 请求复用它

在 `logger/logger.go` 中先解析 `cfg.Level`，再创建 Zap 的 JSON 编码器、Lumberjack 写入器和 `zapcore.Core`。`logger.New` 同时返回日志器和清理函数，初始化时会检查日志文件能否打开，入口在所有其他依赖释放后关闭日志文件。

在 `routes/routes.go` 用 `gin.New()` 创建引擎，再依次挂载 `ginzap.Ginzap` 和 `ginzap.RecoveryWithZap`。两个中间件接收入口创建的同一个 `*zap.Logger`，因此请求日志、panic 日志和应用日志使用同一格式、级别和轮转规则。当前按直连处理客户端地址。将来如果接入反向代理，再填写实际可信的代理地址或网段。 [logger/logger.go](logger/logger.go) · [routes/routes.go](routes/routes.go)

### 5. 初始化数据库依赖

在 `dao/mysql/mysql.go` 使用 MySQL 驱动的 `NewConfig` 和 `FormatDSN` 生成连接串。导入驱动会注册 `mysql`，然后用 `sql.Open` 创建连接池并应用连接池大小。启动时通过带截止时间的 `PingContext` 验证网络、认证和库名。成功后把 `*sql.DB` 返回给入口。

在 `dao/redis/redis.go` 构造 `redis.Options`，通过 `NewClient` 创建客户端，再用带截止时间的 `Ping` 验证连接。两个模块都会在探测失败时释放刚创建的资源。成功后由入口负责关闭，具体业务可以复用入口传下来的连接池。 [dao/mysql/mysql.go](dao/mysql/mysql.go) · [dao/redis/redis.go](dao/redis/redis.go)

### 6. 在入口装配并处理关机

用完整的 [main.go](main.go) 替换临时入口，按以下顺序组织：

1. 解析 `-config`，订阅退出信号，加载配置并设置 Gin 模式。
2. 创建日志器、MySQL 池和 Redis 客户端。每初始化成功一项就登记清理函数。
3. 用 `routes.Setup(log)` 创建 HTTP Handler，配置请求头读取超时和空闲连接超时。
4. 用 `net.Listen` 绑定端口。绑定失败时返回错误，入口输出到标准错误并返回非零退出码。
5. 运行 `http.Server.Serve`。收到退出信号后，用独立的五秒上下文调用 `Shutdown` 等待当前请求结束，超时则关闭连接。
6. 退出时按逆序关闭 Redis、MySQL 和日志输出。

### 7. 格式化、测试和构建

~~~powershell
go fmt ./...
go mod tidy
go test ./...
go vet ./...
go build ./...
~~~

回归测试使用临时文件和回环端口，无需先启动数据库。测试覆盖配置优先级、非法配置和关机时完成在途请求。要验证真实依赖和程序入口，再按运行步骤启动 MySQL、Redis 并访问根路由。

## 常见问题

| 现象 | 检查方式 |
| --- | --- |
| 找不到 `config.yaml` | 检查当前工作目录，或显式传入 `-config`。 |
| `.env` 读取或解析失败 | 检查 YAML 同目录中的文件、访问权限和 `KEY=VALUE` 格式。 |
| 配置出现未知字段 | 对照 YAML 和 `mapstructure` 标签，尤其是带下划线的字段。 |
| MySQL `Access denied` 或 `Unknown database` | 检查应用用户、密码、环境变量覆盖以及数据库是否已创建。 |
| MySQL 或 Redis 连接失败 | 检查容器是否已就绪、宿主机映射以及 YAML 中的目标端口。 |
| HTTP 端口被占用 | 检查是否已经有实例在运行，或把 `app.port` 改成其他可用端口。 |
| 请求成功但终端没有请求日志 | 查看 `log.filename` 指定的文件，确认日志级别允许输出 `info`。 |

接入第一个业务接口时，可以先为路由添加处理器，把使用到的数据库依赖从 `main` 传给 DAO，再由处理器把 `c.Request.Context()` 传给数据库操作。有独立的业务规则时，再引入业务层。
