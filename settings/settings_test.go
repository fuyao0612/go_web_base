// 使用与配置实现相同的包名，测试代码由 Go 工具在运行测试时编译。
package settings

import (
	// os 用于操作测试环境变量和创建临时配置文件。
	"os"
	// filepath 把临时目录与配置文件名拼成当前系统可用的路径。
	"path/filepath"
	// strings 用于生成单个字段错误的配置，以及匹配返回的错误信息。
	"strings"
	// testing 提供临时目录、环境恢复和表驱动子测试功能。
	"testing"
)

// fixtureConfig 包含四个配置块，密码优先级测试使用专用占位值。
// 原始字符串保留多行 YAML 的缩进和换行。两个密码都是用来区分覆盖来源的测试值。
const fixtureConfig = `app:
  name: test-app
  mode: test
  port: 8081
log:
  level: info
  filename: app.log
  max_size: 10
mysql:
  host: localhost
  port: 3306
  user: tester
  dbname: testdb
  password: yaml-only
  max_open_conns: 10
  max_idle_conns: 2
redis:
  host: localhost
  port: 6379
  password: yaml-redis
`

// TestLoadPasswordPrecedence 验证三层配置的顺序、空值覆盖和进程环境隔离。
func TestLoadPasswordPrecedence(t *testing.T) {
	// 先处理可能从开发机或 CI 继承的两个变量，确保测试从真正未设置的状态开始。
	for _, name := range []string{"APP_MYSQL_PASSWORD", "APP_REDIS_PASSWORD"} {
		// 调用 t.Setenv 让测试框架记录原值，并在测试结束后恢复。
		t.Setenv(name, "")
		// 再用 Unsetenv 移除键。空字符串本身仍算一个有效来源，所以仅设置为空不够。
		if err := os.Unsetenv(name); err != nil {
			// 无法清除继承的环境变量时中止，避免来源优先级断言失真。
			t.Fatal(err)
		}
	}

	// 创建隔离目录，测试结束后 Go 会自动删除，真实项目配置不会被读取或覆盖。
	dir := t.TempDir()
	// 用明确的 config.yaml 路径作为 Load 的入口。
	path := filepath.Join(dir, "config.yaml")
	// 把 YAML fixture 写入临时文件。0o600 是创建时请求的文件权限。
	if err := os.WriteFile(path, []byte(fixtureConfig), 0o600); err != nil {
		// 文件写入有错时立即中止，避免把准备失败误判成加载行为错误。
		t.Fatal(err)
	}
	// 先在 .env 缺失的情况下加载，取得结构体配置和错误。
	cfg, err := Load(path)
	// 只有加载成功且 MySQL 密码仍为 YAML 占位值，才能证明缺失 .env 时的回退规则。
	if err != nil || cfg.MySQL.Password != "yaml-only" {
		t.Fatalf("缺少 .env 时应使用 YAML 中的值，错误为 %v", err)
	}

	// 构造两项 .env 测试值，以分别验证进程覆盖和文件覆盖。
	dotenv := "APP_MYSQL_PASSWORD=local-mysql\nAPP_REDIS_PASSWORD=local-redis\n"
	// 将 .env 写在 YAML 同目录，契合 Load 定位凭据文件的规则。
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv), 0o600); err != nil {
		t.Fatal(err)
	}
	// 只设置进程中的 MySQL 密码，让 Redis 仍从 .env 取得密码。
	t.Setenv("APP_MYSQL_PASSWORD", "process-mysql")
	// 重新用同一路径加载，检查当前来源优先级，避免沿用第一次的结果。
	cfg, err = Load(path)
	// 一次断言验证 MySQL 取进程值、Redis 取 .env 值，并检查加载成功。
	if err != nil || cfg.MySQL.Password != "process-mysql" || cfg.Redis.Password != "local-redis" {
		t.Fatalf("进程环境变量应优先于 .env，.env 应优先于 YAML，错误为 %v", err)
	}
	// 即使 Redis 密码已进入 cfg，也不应该被加载函数写到进程环境中。
	if _, exists := os.LookupEnv("APP_REDIS_PASSWORD"); exists {
		// 如果进程里出现该键，说明配置加载产生了意外的全局副作用。
		t.Fatal("加载 .env 不应修改进程环境")
	}

	// 显式设置空字符串，模拟调用者刻意清除下级来源提供的密码。
	t.Setenv("APP_MYSQL_PASSWORD", "")
	// 重新加载以检查空值是否被当成有效的最高优先级覆盖。
	cfg, err = Load(path)
	// 预期结果是空密码。错误或下级来源的非空值都表示行为不符合约定。
	if err != nil || cfg.MySQL.Password != "" {
		t.Fatalf("显式设置的空环境变量应覆盖其他来源，错误为 %v", err)
	}
}

// TestLoadRejectsInvalidConfig 验证错误配置在初始化依赖之前就会被拒绝。
func TestLoadRejectsInvalidConfig(t *testing.T) {
	// 每个表项提供一份独立输入和预期错误，复用文件准备与断言流程。
	for _, test := range []struct {
		// name 用作子测试名，yaml 和 dotenv 是两种文件内容，wantError 是应出现的错误片段。
		name, yaml, dotenv, wantError string
	}{
		// 只改动一个键的拼写，验证严格解码能识别未知键。
		{"未知字段", strings.Replace(fixtureConfig, "max_idle_conns", "max_idle_conn", 1), "", "max_idle_conn"},
		// 保留合法键但让空闲连接数超过总数，验证语义校验生效。
		{"空闲连接超限", strings.Replace(fixtureConfig, "max_idle_conns: 2", "max_idle_conns: 20", 1), "", "mysql.max_idle_conns"},
		// 保留合法 YAML，加入缺少右引号的 .env，验证文件解析失败会被报告。
		{"损坏的环境文件", fixtureConfig, "APP_MYSQL_PASSWORD='missing-quote", ".env"},
	} {
		// 为每种输入运行单独的子测试，失败时能直接看到对应场景名。
		t.Run(test.name, func(t *testing.T) {
			// 为当前子测试创建独立目录，避免一个用例的 .env 影响另一个用例。
			dir := t.TempDir()
			// 拼接当前子测试专用的 YAML 路径。
			path := filepath.Join(dir, "config.yaml")
			// 写入本用例的 YAML 内容，读写环境准备错误直接视为测试失败。
			if err := os.WriteFile(path, []byte(test.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			// 只有用例提供 dotenv 内容时才创建该可选文件。
			if test.dotenv != "" {
				// 按当前用例的原始文本写入文件，故意损坏的内容交给 Load 自己解析。
				if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(test.dotenv), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// 调用与生产入口相同的加载函数，只检查错误，因此丢弃返回的配置。
			_, err := Load(path)
			// 必须返回错误，且错误应明确指向预期问题。用片段匹配允许外层补充路径等信息。
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				// 断言失败时同时显示预期片段和实际错误，便于区分漏报与误报。
				t.Fatalf("应返回包含 %q 的错误，实际为 %v", test.wantError, err)
			}
		})
	}
}
