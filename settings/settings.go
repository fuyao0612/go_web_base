// Package settings 读取并校验应用启动时所需的配置。
package settings

import (
	// errors.Is 用于通过错误链判断可选文件是否确实不存在。
	"errors"
	// fmt 为加载和校验错误补充配置路径或字段名。
	"fmt"
	// os.LookupEnv 用于区分变量未设置和已设置为空字符串。
	"os"
	// filepath 按当前操作系统的路径规则提取目录并拼接文件名。
	"path/filepath"
	// strings 用于在校验必填文本时排除只含空白的值。
	"strings"

	// godotenv 解析本地 .env 文件，Viper 负责读取 YAML、合并覆盖项和解码结构体。
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// Config 对应 YAML 的四个顶层配置块。main 按模块传递各块配置。
type Config struct {
	// 接收 YAML 顶层 app，传给入口用于模式映射、监听端口和启动日志。
	App AppConfig `mapstructure:"app"`
	// 接收 YAML 顶层 log，交给日志模块创建输出。
	Log LogConfig `mapstructure:"log"`
	// 接收 YAML 顶层 mysql，交给 MySQL 模块建立连接池。
	MySQL MySQLConfig `mapstructure:"mysql"`
	// 接收 YAML 顶层 redis，交给 Redis 模块创建客户端。
	Redis RedisConfig `mapstructure:"redis"`
}

// AppConfig 定义应用名称、Gin 运行模式和 HTTP 监听端口。
type AppConfig struct {
	// Name 是应用名，入口把它作为启动日志中的 app 字段。
	Name string `mapstructure:"name"`
	// Mode 使用 dev、test 或 prod，由入口映射成 Gin 的三个模式。
	Mode string `mapstructure:"mode"`
	// Port 是本服务的 TCP 端口，启动校验要求在 1 到 65535 之间。
	Port int `mapstructure:"port"`
}

// LogConfig 定义日志级别、输出路径和日志轮转规则。
type LogConfig struct {
	// Level 是允许输出的最低级别，例如 debug 或 info。Zap 会解析并检查它。
	Level string `mapstructure:"level"`
	// Filename 是日志路径，相对路径以进程工作目录为准。
	Filename string `mapstructure:"filename"`
	// 达到 MaxSize 后，Lumberjack 将继续写入轮转后的新日志文件。
	MaxSize int `mapstructure:"max_size"` // 单文件轮转阈值，单位为 MB。
	// MaxAge 按历史日志文件的轮转日期计算。
	MaxAge int `mapstructure:"max_age"` // 历史文件保留天数，0 表示不限。
	// 超过 MaxBackups 时，最旧的一批历史日志会被清理。
	MaxBackups int `mapstructure:"max_backups"` // 历史文件数量上限，0 表示不限。
}

// MySQLConfig 定义数据库连接和 database/sql 连接池参数。
type MySQLConfig struct {
	// Host 保存主机名或 IP，端口在连接模块中单独拼接。
	Host string `mapstructure:"host"`
	// Port 是 MySQL 服务端口，使用容器时填写应用进程实际访问的宿主机端口。
	Port int `mapstructure:"port"`
	// User 是 MySQL 登录账户，需要能访问 DBName 指定的库。
	User string `mapstructure:"user"`
	// Password 可由 APP_MYSQL_PASSWORD 覆盖，实际密码应避免提交到仓库。
	Password string `mapstructure:"password"`
	// DBName 是连接建立后选用的数据库，需要提前创建。
	DBName string `mapstructure:"dbname"`
	// MaxOpenConns 同时计入正在使用和空闲的连接。
	MaxOpenConns int `mapstructure:"max_open_conns"` // 0 表示不限。
	// MaxIdleConns 控制池中可以保留多少个等待复用的连接。
	MaxIdleConns int `mapstructure:"max_idle_conns"` // 0 表示不保留空闲连接。
}

// RedisConfig 定义 Redis 地址、逻辑数据库和客户端连接池参数。
type RedisConfig struct {
	// Host 指向可访问的 Redis 实例。
	Host string `mapstructure:"host"`
	// Port 是 Redis 监听端口。容器映射场景填应用侧能够访问的端口。
	Port int `mapstructure:"port"`
	// Password 可由 APP_REDIS_PASSWORD 覆盖，无密码实例可以留空。
	Password string `mapstructure:"password"`
	// DB 是 Redis 的逻辑库编号，从 0 开始。
	DB int `mapstructure:"db"`
	// PoolSize 设置客户端连接池的基础大小。
	PoolSize int `mapstructure:"pool_size"` // 0 使用客户端默认值。
}

// Load 读取 YAML 和同目录下可选的 .env，返回一次启动所用的配置快照。
// 密码优先取进程环境变量，其次取 .env，最后使用 YAML。
func Load(path string) (Config, error) {
	// 每次加载创建私有 Viper 实例，避免依赖包级 Viper 状态或先前读取的配置。
	v := viper.New()
	// 明确指定文件路径。相对路径由当前进程工作目录解析。
	v.SetConfigFile(path)
	// 读取并解析文件。该 if 中的 err 仅用于当前分支判断。
	if err := v.ReadInConfig(); err != nil {
		// 返回空配置和带路径的错误。%w 保留底层读文件或 YAML 解析失败的原因。
		return Config{}, fmt.Errorf("读取配置 %q: %w", path, err)
	}

	// Read 把文件解析成 map，避免修改进程环境，也便于独立验证优先级。
	// 先取 YAML 的父目录，再拼出同目录 .env，-config 指到其他目录时规则也一致。
	envPath := filepath.Join(filepath.Dir(path), ".env")
	// 解析得到键值 map 或错误，调用不会设置进程环境变量。
	dotenv, err := godotenv.Read(envPath)
	// 文件不存在时继续使用其他来源。权限错误、格式损坏等问题仍会中止加载。
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		// dotenv 解析错误可能附带原始文件内容，避免把密码带到 stderr。
		return Config{}, fmt.Errorf("读取或解析 %q 失败，请检查文件权限和 KEY=VALUE 格式", envPath)
	}

	// 每条映射把 Viper 的层级配置键对应到一个环境变量名。两个键独立覆盖。
	for key, variable := range map[string]string{
		// 把 MySQL 凭据的环境变量对应到 mysql 块的 password 字段。
		"mysql.password": "APP_MYSQL_PASSWORD",
		// 把 Redis 凭据的环境变量对应到 redis 块的 password 字段。
		"redis.password": "APP_REDIS_PASSWORD",
	} {
		// 优先检查进程环境。exists 表示键存在，即使 value 是空字符串也成立。
		if value, exists := os.LookupEnv(variable); exists {
			// Viper 的 Set 把该键设为覆盖值，因此会优先于已经读入的 YAML。
			v.Set(key, value)
			// 只有进程环境没有对应键时，才查询本地 .env 的 map。
		} else if value, exists := dotenv[variable]; exists {
			// 使用 .env 提供的值覆盖 YAML。两个来源都没有这个键时保留 YAML。
			v.Set(key, value)
		}
	}

	// 使用严格解码，配置键拼写有误时立即报告。
	// 先声明零值结构体，然后把它的地址传给解码器以填充字段。
	var cfg Config
	// UnmarshalExact 按 mapstructure 标签解码。未知键或无法转换的值会返回错误。
	if err := v.UnmarshalExact(&cfg); err != nil {
		// 附上文件路径并返回空结构体，避免调用方拿到只填了一部分的数据。
		return Config{}, fmt.Errorf("解析配置 %q: %w", path, err)
	}
	// 解码成功也可能包含超范围的端口或其他无效值，因此再检查业务约束。
	if err := cfg.validate(); err != nil {
		// validate 已经指出问题字段，直接返回它的错误和空配置。
		return Config{}, err
	}
	// 所有步骤成功后按值返回结构体快照。nil 表示没有加载错误。
	return cfg, nil
}

// validate 提前识别会导致启动失败或被客户端静默修正的配置。
func (cfg Config) validate() error {
	// 把三个服务端口放在一起校验。循环顺序确定，因此先报告第一个不合法的键。
	for _, entry := range []struct {
		// key 保存面向用户显示的 YAML 路径。
		key string
		// port 保存从结构体读到的整数，缺少字段时为零值。
		port int
	}{
		{"app.port", cfg.App.Port},
		{"mysql.port", cfg.MySQL.Port},
		{"redis.port", cfg.Redis.Port},
	} {
		// 要求显式配置可用的端口号。这里也拒绝会让监听器选择随机端口的 0。
		if entry.port < 1 || entry.port > 65535 {
			// 把出错键和实际值带到错误信息中，方便直接查配置。
			return fmt.Errorf("%s 必须在 1 到 65535 之间，当前为 %d", entry.key, entry.port)
		}
	}

	// 把要求非空的名称、路径、地址和账号集中检查。密码不在这个必填列表中。
	for _, entry := range []struct {
		// key 指明要在错误里报告的配置项。
		key string
		// value 是待检查的原始文本。
		value string
	}{
		{"app.name", cfg.App.Name},
		{"log.filename", cfg.Log.Filename},
		{"mysql.host", cfg.MySQL.Host},
		{"mysql.user", cfg.MySQL.User},
		{"mysql.dbname", cfg.MySQL.DBName},
		{"redis.host", cfg.Redis.Host},
	} {
		// TrimSpace 用于识别空字符串及仅含空白的字符串。此处不会改写原始配置。
		if strings.TrimSpace(entry.value) == "" {
			// 发现第一个空字段就返回它的完整配置键。
			return fmt.Errorf("%s 不能为空", entry.key)
		}
	}

	// 单文件阈值必须为正数，天数和备份数量允许以 0 表示不限。
	if cfg.Log.MaxSize <= 0 || cfg.Log.MaxAge < 0 || cfg.Log.MaxBackups < 0 {
		return fmt.Errorf("log.max_size 必须大于 0，log.max_age 和 log.max_backups 必须大于等于 0")
	}
	// 连接数参数允许为 0，但负值通常表示配置填写错误。
	if cfg.MySQL.MaxOpenConns < 0 || cfg.MySQL.MaxIdleConns < 0 {
		return fmt.Errorf("MySQL 连接池参数必须大于等于 0")
	}
	// 只有总连接数有限制时才比较两者，避免 database/sql 自动下调空闲上限。
	if cfg.MySQL.MaxOpenConns > 0 && cfg.MySQL.MaxIdleConns > cfg.MySQL.MaxOpenConns {
		return fmt.Errorf("mysql.max_idle_conns 不能超过 mysql.max_open_conns")
	}
	// Redis 逻辑库和池大小都要求非负。池大小为 0 留给客户端选择默认值。
	if cfg.Redis.DB < 0 || cfg.Redis.PoolSize < 0 {
		return fmt.Errorf("redis.db 和 redis.pool_size 必须大于等于 0")
	}
	// 通过本函数的全部检查，返回 nil 让 Load 交付配置。
	return nil
}
