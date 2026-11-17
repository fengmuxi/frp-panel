package conf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	"github.com/gin-gonic/gin"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
	"github.com/tidwall/pretty"
)

type Config struct {
	App struct {
		UseGvisorNet bool   `env:"USE_GVISOR_NET" env-default:"false" env-description:"use gvisor netstack for TUN device"`
		GlobalSecret string `env:"GLOBAL_SECRET" env-default:"frp-panel" env-description:"global secret, used in manager gen secret, keep it safe"`
		// RequireStrongSecret 安全基线开关：拒绝默认/过短 GLOBAL_SECRET 启动（默认开启）。
		// 仅应作为历史部署（仍使用默认值）的过渡兼容手段——尽快经 frpp migrate rekey
		// 换成强随机值后移除该开关，弱钥派生的 JWT 签名密钥可被离线推导
		RequireStrongSecret bool   `env:"REQUIRE_STRONG_SECRET" env-default:"true" env-description:"reject default/weak GLOBAL_SECRET at master startup; set false only to keep legacy deployments booting (rotate via frpp migrate rekey)"`
		CookieAge           int    `env:"COOKIE_AGE" env-default:"86400" env-description:"cookie age in second, default is 1 day"`
		CookieName          string `env:"COOKIE_NAME" env-default:"frp-panel-cookie" env-description:"cookie name"`
		CookiePath          string `env:"COOKIE_PATH" env-default:"/" env-description:"cookie path"`
		CookieDomain        string `env:"COOKIE_DOMAIN" env-default:"" env-description:"cookie domain"`
		CookieSecure        bool   `env:"COOKIE_SECURE" env-default:"true" env-description:"cookie secure (安全审计 P1-8：默认收紧为 true；纯 HTTP 部署请改用反代 TLS 或显式回退)"`
		CookieHTTPOnly      bool   `env:"COOKIE_HTTP_ONLY" env-default:"true" env-description:"cookie http only"`
		EnableRegister      bool   `env:"ENABLE_REGISTER" env-default:"false" env-description:"enable register, only allow the first admin to register"`
		GithubProxyUrl      string `env:"GITHUB_PROXY_URL" env-default:"https://ghfast.top/" env-description:"github proxy url"`
	} `env-prefix:"APP_"`
	Master struct {
		APIPort int `env:"API_PORT" env-default:"9000" env-description:"master api port"`
		// APIListenIP 是 API/WebUI 的监听 IP：默认 0.0.0.0 监听所有网卡（含公网）。
		// 只做本机反代/本机访问时建议改为 127.0.0.1，避免面板直接暴露公网
		APIListenIP string `env:"API_LISTEN_IP" env-default:"0.0.0.0" env-description:"panel api/webui listen ip, 0.0.0.0 = all interfaces; set 127.0.0.1 to bind loopback only (e.g. behind a local reverse proxy)"`
		APIHost     string `env:"API_HOST" env-description:"master host, can behind proxy like cdn"`
		APIScheme   string `env:"API_SCHEME" env-default:"http" env-description:"master api scheme"`
		CacheSize   int    `env:"CACHE_SIZE" env-default:"10" env-description:"cache size in MB"`
		// RPCListenIP 是节点 RPC（wsgrpc）的监听 IP：默认 0.0.0.0 让节点可从任意地址连入；
		// 节点全部经域名反代/CDN 接入时可改 127.0.0.1 收紧
		RPCListenIP           string `env:"RPC_LISTEN_IP" env-default:"0.0.0.0" env-description:"node rpc (wsgrpc) listen ip, 0.0.0.0 = all interfaces; set 127.0.0.1 when nodes reach master via reverse proxy only"`
		RPCHost               string `env:"RPC_HOST" env-default:"127.0.0.1" env-description:"master host, is a public ip or domain"`
		RPCPort               int    `env:"RPC_PORT" env-default:"9001" env-description:"master rpc port"`
		InternalFRPServerHost string `env:"INTERNAL_FRP_SERVER_HOST" env-description:"internal frp server host, used for client connection"`
	} `env-prefix:"MASTER_"`
	Server struct {
		APIPort int `env:"API_PORT" env-default:"8999" env-description:"server api port"`
	} `env-prefix:"SERVER_"`
	DB struct {
		Type string `env:"TYPE" env-default:"sqlite3" env-description:"db type, mysql or sqlite3 and so on"`
		DSN  string `env:"DSN" env-default:"/data/data.db?_pragma=journal_mode(WAL)" env-description:"db dsn, for sqlite is path, other is dsn, look at https://github.com/go-sql-driver/mysql#dsn-data-source-name"`
	} `env-prefix:"DB_"`
	Client struct {
		ID                    string `env:"ID" env-description:"client id"`
		Secret                string `env:"SECRET" env-description:"client secret"`
		TLSRpc                bool   `env:"TLS_RPC" env-default:"true" env-description:"use tls for rpc connection"`
		RPCUrl                string `env:"RPC_URL" env-description:"rpc url, support ws or wss or grpc scheme, eg: ws://127.0.0.1:9000"`
		APIUrl                string `env:"API_URL" env-description:"api url, support http or https scheme, eg: http://127.0.0.1:9000"`
		TLSInsecureSkipVerify bool   `env:"TLS_INSECURE_SKIP_VERIFY" env-default:"false" env-description:"skip tls verify (安全审计 P1-10：默认收紧为验证模式；自签部署需将 master CA 纳入信任或显式回退)"`
		Worker                struct {
			WorkerdBinaryPath  string `env:"WORKERD_BINARY_PATH" env-description:"workerd binary path"`
			WorkerdWorkDir     string `env:"WORKERD_WORK_DIR" env-default:"/tmp/frpp/workerd" env-description:"workerd work dir"`
			WorkerdDownloadURL struct {
				UseProxy   bool   `env:"USE_PROXY" env-default:"true" env-description:"use proxy"`
				LinuxArm64 string `env:"LINUX_ARM64" env-default:"https://github.com/cloudflare/workerd/releases/download/v1.20250505.0/workerd-linux-arm64.gz"`
				LinuxX8664 string `env:"LINUX_X86_64" env-default:"https://github.com/cloudflare/workerd/releases/download/v1.20250505.0/workerd-linux-64.gz"`
			} `env-prefix:"WORKERD_DOWNLOAD_URL_" env-description:"workerd download url"`
		} `env-prefix:"WORKER_" env-description:"worker's config"`
		Features struct {
			EnableFunctions   bool `env:"ENABLE_FUNCTIONS" env-default:"true" env-description:"enable functions"`
			EnableRemoteShell bool `env:"ENABLE_REMOTE_SHELL" env-default:"true" env-description:"enable remote shell"`
		} `env-prefix:"FEATURES_" env-description:"features config"`
	} `env-prefix:"CLIENT_"`
	IsDebug bool `env:"IS_DEBUG" env-default:"false" env-description:"is debug mode"`
	Debug   struct {
		ProfilerEnabled bool `env:"PROFILER_ENABLED" env-default:"false" env-description:"enable profiler"`
		ProfilerPort    int  `env:"PROFILER_PORT" env-default:"6961" env-description:"profiler port"`
	} `env-prefix:"DEBUG_"`
	Logger struct {
		DefaultLoggerLevel string `env:"DEFAULT_LOGGER_LEVEL" env-default:"info" env-description:"frp-panel internal default logger level"`
		FRPLoggerLevel     string `env:"FRP_LOGGER_LEVEL" env-default:"info" env-description:"frp logger level"`
	} `env-prefix:"LOGGER_"`
	HTTP_PROXY string `env:"HTTP_PROXY" env-description:"http proxy"`
}

func NewConfig() Config {
	var (
		err        error
		useEnvFile bool
		ctx        = context.Background()
	)

	// 越前面优先级越高，后面的不会覆盖前面的
	envFiles := []string{
		defs.CurEnvPath,
		defs.SysEnvPath,
	}

	for _, envFile := range envFiles {
		if err = godotenv.Load(envFile); err == nil {
			logger.Logger(ctx).Infof("load env file success: %s", envFile)
			useEnvFile = true
		}
	}

	if !useEnvFile {
		logger.Logger(ctx).Info("use runtime env variables")
	}

	cfg := Config{}
	if err = cleanenv.ReadEnv(&cfg); err != nil {
		logger.Logger(ctx).Panic(err)
	}
	cfg.Complete()

	if !cfg.IsDebug {
		gin.SetMode(gin.ReleaseMode)
	}

	return cfg
}

// GlobalSecretWeak 判断 GLOBAL_SECRET 是否为弱值（默认值或长度不足）。
// 校验开关关闭时调用方应据此降级告警而非拒绝启动。
func (cfg *Config) GlobalSecretWeak() bool {
	return cfg.App.GlobalSecret == "" || cfg.App.GlobalSecret == "frp-panel" || len(cfg.App.GlobalSecret) < 16
}

// ValidateGlobalSecret 校验 GLOBAL_SECRET 安全基线（安全审计 P1-4）：
// 默认值 "frp-panel" 或过短密钥会让 JWT 签名密钥（SHA1(APIHost:APIPort:GLOBAL_SECRET)）
// 可被任何知道面板域名的人离线推导，进而伪造管理员 token。master 启动时默认必须通过本校验。
//
// 兼容口：REQUIRE_STRONG_SECRET=false 可放宽本校验（仍拒绝空值），供仍在使用
// 默认/短密钥的历史部署继续启动。这只是过渡手段——弱钥风险依然存在，应在完成
// `frpp migrate rekey` 轮换后移除开关；frpp check env 会持续降级告警。
func (cfg *Config) ValidateGlobalSecret() error {
	if cfg.App.GlobalSecret == "" {
		return fmt.Errorf("APP_GLOBAL_SECRET is empty")
	}
	if !cfg.App.RequireStrongSecret {
		if cfg.GlobalSecretWeak() {
			logger.Logger(context.Background()).Warnf("strong-secret check disabled (REQUIRE_STRONG_SECRET=false): " +
				"APP_GLOBAL_SECRET is weak, JWT signing key can be derived offline; " +
				"rotate via `frpp migrate rekey` and re-enable the check")
		}
		return nil
	}
	if cfg.App.GlobalSecret == "frp-panel" {
		return fmt.Errorf("APP_GLOBAL_SECRET is still the default value; it derives the JWT signing key, refusing to start " +
			"(legacy deployments can temporarily set APP_REQUIRE_STRONG_SECRET=false, but rotation via `frpp migrate rekey` is strongly recommended)")
	}
	if len(cfg.App.GlobalSecret) < 16 {
		return fmt.Errorf("APP_GLOBAL_SECRET too short (%d chars, >=16 required)", len(cfg.App.GlobalSecret))
	}
	return nil
}

func (cfg *Config) Complete() {
	if len(cfg.Master.InternalFRPServerHost) == 0 {
		cfg.Master.InternalFRPServerHost = cfg.Master.RPCHost
	}

	if len(cfg.Master.APIHost) == 0 {
		cfg.Master.APIHost = cfg.Master.RPCHost
	}

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if len(cfg.Client.ID) == 0 {
		cfg.Client.ID = hostname
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("failed to get current working directory:", err)
		os.Exit(1)
	}

	if len(cfg.Client.Worker.WorkerdBinaryPath) == 0 {
		w, _ := utils.FindExecutableNames(func(name string) bool {
			return strings.HasPrefix(name, "workerd")
		}, cwd, "/")
		if len(w) > 0 {
			cfg.Client.Worker.WorkerdBinaryPath = w[0]
		}
	}
}

func (cfg Config) PrintStr() string {
	raw, _ := json.Marshal(cfg)
	return string(pretty.Pretty(raw))
}
