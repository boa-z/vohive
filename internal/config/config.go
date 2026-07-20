package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const (
	ESIMTransportAT            = "at"
	ESIMTransportQMI           = "qmi"
	ESIMTransportMBIM          = "mbim"
	ModuleVendorQuectel        = "quectel"
	ModuleVendorSIMCOM         = "simcom"
	MBIMTransportAuto          = "auto"
	MBIMTransportProxy         = "proxy"
	MBIMTransportDirect        = "direct"
	DefaultFreeDeviceLimit     = 5
	DefaultWebhookTextTemplate = "{{device_label}} {{text}}"
)

func NormalizeESIMTransport(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", ESIMTransportAT:
		return ESIMTransportAT
	case ESIMTransportQMI:
		return ESIMTransportQMI
	case ESIMTransportMBIM:
		return ESIMTransportMBIM
	default:
		return strings.ToLower(strings.TrimSpace(in))
	}
}

func ValidateESIMTransport(in string) error {
	switch NormalizeESIMTransport(in) {
	case ESIMTransportAT, ESIMTransportQMI, ESIMTransportMBIM:
		return nil
	default:
		return fmt.Errorf("invalid esim transport: %q", strings.TrimSpace(in))
	}
}

func NormalizeModuleVendor(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", "auto", ModuleVendorQuectel:
		return ModuleVendorQuectel
	case ModuleVendorSIMCOM, "sim-com", "sim_com":
		return ModuleVendorSIMCOM
	default:
		return strings.ToLower(strings.TrimSpace(in))
	}
}

func ValidateModuleVendor(in string) error {
	switch NormalizeModuleVendor(in) {
	case ModuleVendorQuectel, ModuleVendorSIMCOM:
		return nil
	default:
		return fmt.Errorf("invalid module vendor: %q", strings.TrimSpace(in))
	}
}

func NormalizeMBIMTransport(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", MBIMTransportAuto:
		return MBIMTransportAuto
	case MBIMTransportProxy:
		return MBIMTransportProxy
	case MBIMTransportDirect:
		return MBIMTransportDirect
	default:
		return MBIMTransportAuto
	}
}

// ResolveIPFamily parses DeviceConfig.IPVersion into IPv4/IPv6 enable flags.
// Empty input preserves the legacy IPv4-only behavior.
func ResolveIPFamily(in string) (enableV4 bool, enableV6 bool, err error) {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", "v4", "ipv4":
		return true, false, nil
	case "v6", "ipv6":
		return false, true, nil
	case "v4v6", "v6v4", "dual", "ipv4v6":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("无效的 ip_version: %q (允许 v4|v6|v4v6)", in)
	}
}

type Config struct {
	Server          ServerConfig   `mapstructure:"server"`
	FreeDeviceLimit int            `mapstructure:"free_device_limit"` // 0 means unlimited
	Devices         []DeviceConfig `mapstructure:"devices"`
	Telegram        TelegramConfig `mapstructure:"telegram"`
	Feishu          FeishuConfig   `mapstructure:"feishu"`
	QQ              QQConfig       `mapstructure:"qq"`
	Webhook         WebhookConfig  `mapstructure:"webhook"`

	Bark     BarkConfig     `mapstructure:"bark"`
	Email    EmailConfig    `mapstructure:"email"`
	Pushplus PushplusConfig `mapstructure:"pushplus"`
	Web      WebConfig      `mapstructure:"web"`
	Proxy    ProxyConfig    `mapstructure:"proxy"`
	VoWiFi   VoWiFiConfig   `mapstructure:"vowifi"`
}

// ProxyConfig 定义代理服务配置
type ProxyConfig struct {
	Instances []ProxyInstance `mapstructure:"instances"` // 代理实例列表
}

type VoWiFiConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	DeviceID string `mapstructure:"device_id"` // 留空则取第一个
	Mode     string `mapstructure:"mode"`      // vowifi|volte(当前会回退为 vowifi)，默认 vowifi

	VoiceGateway VoWiFiVoiceGatewayConfig `mapstructure:"voice_gateway"`
}

// VoWiFiVoiceGatewayConfig 语音网关配置（支持 Linphone 接打电话）。
// 无需显式 enabled 字段，sip.listen 非空时自动启用。
type VoWiFiVoiceGatewayConfig struct {
	// SIP 服务配置
	SIP struct {
		Listen     string `mapstructure:"listen"`      // 监听地址，如 "0.0.0.0:5060"
		Transport  string `mapstructure:"transport"`   // 传输协议: udp/tcp/tls
		Realm      string `mapstructure:"realm"`       // SIP 认证域
		ExternalIP string `mapstructure:"external_ip"` // 公网 IP (可选)
	} `mapstructure:"sip"`

	// 用户配置
	Users []VoWiFiVoiceUserConfig `mapstructure:"users"`

	// 媒体配置
	Media struct {
		RTPPortMin int      `mapstructure:"rtp_port_min"`
		RTPPortMax int      `mapstructure:"rtp_port_max"`
		Codecs     []string `mapstructure:"codecs"`
	} `mapstructure:"media"`

	// 推送配置 (Linphone APNs/FCM)
	LinphonePush struct {
		LinphoneUser     string `mapstructure:"linphone_user"`
		LinphonePassword string `mapstructure:"linphone_password"`
	} `mapstructure:"linphone_push"`
}

// VoWiFiVoiceUserConfig 语音用户配置
type VoWiFiVoiceUserConfig struct {
	Username    string `mapstructure:"username"`
	Password    string `mapstructure:"password"`
	DisplayName string `mapstructure:"display_name"`
	DeviceID    string `mapstructure:"device_id"` // 绑定的设备 ID
}

// ProxyInstance 定义一个代理实例配置
type ProxyInstance struct {
	ID          string `mapstructure:"id" json:"id"`                   // 实例唯一标识
	Name        string `mapstructure:"name" json:"name"`               // 显示名称
	DeviceID    string `mapstructure:"device_id" json:"device_id"`     // 绑定设备 ID（强制绑定对应网卡）
	Enabled     bool   `mapstructure:"enabled" json:"enabled"`         // 是否启用
	Mode        string `mapstructure:"mode" json:"mode"`               // 代理模式: socks5|http
	ListenAddr  string `mapstructure:"listen_addr" json:"listen_addr"` // 监听地址
	ListenPort  int    `mapstructure:"listen_port" json:"listen_port"` // 监听端口
	AuthEnabled bool   `mapstructure:"auth_enabled" json:"auth_enabled"`
	Username    string `mapstructure:"username" json:"username"`
	Password    string `mapstructure:"password" json:"password"`
}

type V2RayTransportOptions struct {
	HTTP        *V2RayHTTPOptions        `mapstructure:"http,omitempty" json:"http,omitempty"`
	WS          *V2RayWebsocketOptions   `mapstructure:"ws,omitempty" json:"ws,omitempty"`
	GRPC        *V2RayGRPCOptions        `mapstructure:"grpc,omitempty" json:"grpc,omitempty"`
	HTTPUpgrade *V2RayHTTPUpgradeOptions `mapstructure:"httpupgrade,omitempty" json:"httpupgrade,omitempty"`
	QUIC        *V2RayQUICOptions        `mapstructure:"quic,omitempty" json:"quic,omitempty"`
}

type V2RayHTTPOptions struct {
	Host        []string          `mapstructure:"host,omitempty" json:"host,omitempty"`
	Path        string            `mapstructure:"path,omitempty" json:"path,omitempty"`
	Method      string            `mapstructure:"method,omitempty" json:"method,omitempty"`
	Headers     map[string]string `mapstructure:"headers,omitempty" json:"headers,omitempty"`
	IdleTimeout string            `mapstructure:"idle_timeout,omitempty" json:"idle_timeout,omitempty"`
	PingTimeout string            `mapstructure:"ping_timeout,omitempty" json:"ping_timeout,omitempty"`
}

type V2RayWebsocketOptions struct {
	Path                string            `mapstructure:"path,omitempty" json:"path,omitempty"`
	Headers             map[string]string `mapstructure:"headers,omitempty" json:"headers,omitempty"`
	MaxEarlyData        uint32            `mapstructure:"max_early_data,omitempty" json:"max_early_data,omitempty"`
	EarlyDataHeaderName string            `mapstructure:"early_data_header_name,omitempty" json:"early_data_header_name,omitempty"`
}

type V2RayQUICOptions struct{}

type V2RayGRPCOptions struct {
	ServiceName         string `mapstructure:"service_name,omitempty" json:"service_name,omitempty"`
	IdleTimeout         string `mapstructure:"idle_timeout,omitempty" json:"idle_timeout,omitempty"`
	PingTimeout         string `mapstructure:"ping_timeout,omitempty" json:"ping_timeout,omitempty"`
	PermitWithoutStream bool   `mapstructure:"permit_without_stream,omitempty" json:"permit_without_stream,omitempty"`
}

type V2RayHTTPUpgradeOptions struct {
	Host    string            `mapstructure:"host,omitempty" json:"host,omitempty"`
	Path    string            `mapstructure:"path,omitempty" json:"path,omitempty"`
	Headers map[string]string `mapstructure:"headers,omitempty" json:"headers,omitempty"`
}

// InboundUser 定义入站用户配置
type InboundUser struct {
	Name     string `mapstructure:"name" json:"name"`                             // 用户名
	UUID     string `mapstructure:"uuid,omitempty" json:"uuid,omitempty"`         // UUID（VMess/VLESS）
	Password string `mapstructure:"password,omitempty" json:"password,omitempty"` // 密码
	Flow     string `mapstructure:"flow,omitempty" json:"flow,omitempty"`         // VLESS Flow (如 xtls-rprx-vision)
	AlterID  int    `mapstructure:"alter_id,omitempty" json:"alter_id,omitempty"` // AlterID
}

// TLSConfig 定义 TLS 配置
type TLSConfig struct {
	Enabled    bool   `mapstructure:"enabled" json:"enabled"`                             // 是否启用 TLS
	ServerName string `mapstructure:"server_name,omitempty" json:"server_name,omitempty"` // 客户端 SNI / REALITY ServerName
	CertPath   string `mapstructure:"cert_path,omitempty" json:"cert_path,omitempty"`     // 证书文件路径
	KeyPath    string `mapstructure:"key_path,omitempty" json:"key_path,omitempty"`       // 私钥文件路径
	Insecure   bool   `mapstructure:"insecure,omitempty" json:"insecure,omitempty"`       // 是否跳过证书验证

	// REALITY 特有配置
	Reality *RealityConfig `mapstructure:"reality,omitempty" json:"reality,omitempty"`
	// ECH 特有配置
	ECH *ECHConfig `mapstructure:"ech,omitempty" json:"ech,omitempty"`
}

// RealityConfig 定义 REALITY 配置
type RealityConfig struct {
	Enabled     bool     `mapstructure:"enabled" json:"enabled"`
	Handshake   string   `mapstructure:"handshake" json:"handshake"`                         // 握手服务器 (SNI)
	Port        int      `mapstructure:"port" json:"port"`                                   // 握手端口
	PrivateKey  string   `mapstructure:"private_key" json:"private_key"`                     // 私钥
	PublicKey   string   `mapstructure:"public_key" json:"public_key"`                       // 公钥
	ShortID     []string `mapstructure:"short_id" json:"short_id"`                           // Short IDs
	Fingerprint string   `mapstructure:"fingerprint,omitempty" json:"fingerprint,omitempty"` // uTLS 指纹 (fp)
	MaxTimeDiff int64    `mapstructure:"max_time_diff,omitempty" json:"max_time_diff,omitempty"`
}

// ECHConfig 定义 ECH 配置
type ECHConfig struct {
	Enabled bool   `mapstructure:"enabled" json:"enabled"`
	Key     string `mapstructure:"key" json:"key"`       // ECH Keys (Base64)
	Config  string `mapstructure:"config" json:"config"` // ECH Configs (Base64)
}

type WebConfig struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type ServerConfig struct {
	Port  string `mapstructure:"port"`
	Debug bool   `mapstructure:"debug"`
}

type ESIMSwitchConfig struct {
	// UseRefreshTrue uses refresh=true for the main switch path. Default false preserves current behavior.
	UseRefreshTrue bool `mapstructure:"use_refresh_true"`
	// EventGatedConverge uses UIM indication events to gate post-switch convergence. Default false.
	EventGatedConverge bool `mapstructure:"event_gated_converge"`
	// RadioCycle performs LowPower -> Online radio cycling around switch. Default false.
	RadioCycle bool `mapstructure:"radio_cycle"`
	// ReinitWindowMS is the expected UIM reinitialization window in milliseconds. Default 0 disables the window.
	// Only effective when EventGatedConverge=true; ReinitWindow marks the period during which GetUIMReadiness
	// timeouts do not trigger whole-core recovery (to avoid triggering on firmware reinitialization stalls).
	// If EventGatedConverge=false, ReinitWindowMS is silently ignored.
	ReinitWindowMS int `mapstructure:"reinit_window_ms"`
	// NASAttachTimeoutMS bounds optional attach waiting after Online in milliseconds. Default 0 means do not block.
	NASAttachTimeoutMS int `mapstructure:"nas_attach_timeout_ms"`
}

type DeviceConfig struct {
	ID            string `mapstructure:"id"`
	Name          string `mapstructure:"name"` // 设备显示名称
	ModemIMEI     string `mapstructure:"modem_imei"`
	USBPath       string `mapstructure:"-"` // Deprecated: 运行时按 IMEI 现解析,绝不从文件读取
	ATPort        string `mapstructure:"-"` // Deprecated: 运行时解析;AT 终端用 Worker.ResolvedATPort()
	ProxyPort     int    `mapstructure:"proxy_port"`
	ManagePort    string `mapstructure:"-"`              // Deprecated: 运行时解析,绝不从文件读取
	Interface     string `mapstructure:"-"`              // Deprecated: 运行时解析,绝不从文件读取
	QMIDevice     string `mapstructure:"-"`              // Deprecated: 运行时解析,绝不从文件读取
	ControlDevice string `mapstructure:"-"`              // Deprecated: 运行时按 IMEI 现解析,绝不从文件读取
	MBIMTransport string `mapstructure:"mbim_transport"` // MBIM 传输: auto|proxy|direct，默认 auto
	QMIUseProxy   bool   `mapstructure:"qmi_use_proxy"`  // 是否通过 libqmi qmi-proxy 打开 QMI 控制口
	// 可选：qmi-proxy abstract socket 名称和可执行文件路径。留空使用 quectel-qmi-go 默认值。
	QMIProxyPath       string `mapstructure:"qmi_proxy_path"`
	QMIProxyExecutable string `mapstructure:"qmi_proxy_executable"`
	ESIMTransport      string `mapstructure:"esim_transport"` // eSIM 传输通道: at|qmi|mbim，默认 at
	DeviceBackend      string `mapstructure:"device_backend"` // 设备后端模式: at|qmi|mbim|auto，默认 at
	ModuleVendor       string `mapstructure:"module_vendor"`  // AT 方言: quectel|simcom，默认 quectel 以保持兼容
	USBNetMode         *int   `mapstructure:"usbnet_mode"`    // 可选：用于校验/设置厂商 USB 网络模式
	// ESIMSwitch controls deterministic eSIM switch behavior. Zero values preserve current behavior.
	ESIMSwitch ESIMSwitchConfig `mapstructure:"esim_switch"`

	OperatorSelectionMode string `mapstructure:"operator_selection_mode"`
	OperatorSelectionPLMN string `mapstructure:"operator_selection_plmn"`
	OperatorSelectionRAT  string `mapstructure:"operator_selection_rat"`

	// Serial config
	BaudRate int    `mapstructure:"baud_rate"`
	DataBits int    `mapstructure:"data_bits"`
	StopBits int    `mapstructure:"stop_bits"`
	Parity   string `mapstructure:"parity"`

	// 以下为运行时有效策略（投影自 card_policies，按 ICCID），不再从配置文件加载
	APN             string `mapstructure:"-"`
	NetworkEnabled  bool   `mapstructure:"-"`
	IPVersion       string `mapstructure:"-"`
	VoWiFiEnabled   bool   `mapstructure:"-"`
	AirplaneEnabled bool   `mapstructure:"-"`
	SMSEnabled      bool   `mapstructure:"-"` // SMS 恒开，运行时强制 true

	// USB Audio (自动发现，无需手动配置)
	AudioDevice string `mapstructure:"-"` // Deprecated: 运行时解析,绝不从文件读取
}

type TelegramConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	BotToken string `mapstructure:"bot_token"`
	ChatID   int64  `mapstructure:"chat_id"`
	AdminID  int64  `mapstructure:"admin_id"`
	BaseURL  string `mapstructure:"base_url"` // 反向代理地址 (例如 https://api.telegram.org/bot%s/%s)
	Proxy    string `mapstructure:"proxy"`    // HTTP 代理地址 (例如 http://127.0.0.1:7890)
}

// FeishuConfig 飞书通知配置
type FeishuConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	AppID     string   `mapstructure:"app_id"`     // 飞书开放平台应用 App ID
	AppSecret string   `mapstructure:"app_secret"` // 飞书开放平台应用 App Secret
	ChatIDs   []string `mapstructure:"chat_ids"`   // 飞书群聊 chat_id 列表
	ChatID    string   `mapstructure:"chat_id"`    // 兼容旧配置：单个 chat_id
}

type QQConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	AppID     string `mapstructure:"app_id"`
	AppSecret string `mapstructure:"app_secret"`
	GroupIDs  string `mapstructure:"group_ids"`  // 逗号分隔的群组 OpenID
	DirectIDs string `mapstructure:"direct_ids"` // 逗号分隔的私聊 OpenID
}

type WebhookConfig struct {
	Enabled      bool              `mapstructure:"enabled"`
	URLs         []string          `mapstructure:"urls"`
	Secret       string            `mapstructure:"secret"`
	TimeoutMs    int               `mapstructure:"timeout_ms"`
	RetryMax     int               `mapstructure:"retry_max"`
	TextTemplate string            `mapstructure:"text_template"`
	Headers      map[string]string `mapstructure:"headers,omitempty" json:"headers,omitempty"`
}

type BarkConfig struct {
	Enabled bool     `mapstructure:"enabled"`
	URLs    []string `mapstructure:"urls"`
	Group   string   `mapstructure:"group"`
	Icon    string   `mapstructure:"icon"`
	Level   string   `mapstructure:"level"`
}

type EmailConfig struct {
	Enabled     bool     `mapstructure:"enabled"`
	UseSSL      bool     `mapstructure:"use_ssl"`
	SMTPHost    string   `mapstructure:"smtp_host"`
	SMTPPort    int      `mapstructure:"smtp_port"`
	Username    string   `mapstructure:"username"`
	Password    string   `mapstructure:"password"`
	FromAddress string   `mapstructure:"from_address"`
	ToAddresses []string `mapstructure:"to_addresses"`
}

type PushplusConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`
	Topic   string `mapstructure:"topic"`
	Channel string `mapstructure:"channel"`
}

func Load(path string) (*Config, error) {
	if err := migrateLegacyManagedNetworkField(path); err != nil {
		return nil, err
	}
	if err := migrateDeprecatedRuntimePathFields(path); err != nil {
		return nil, err
	}

	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	// 默认值设置
	viper.SetDefault("free_device_limit", DefaultFreeDeviceLimit)
	viper.SetDefault("server.port", 7575)
	viper.SetDefault("webhook.timeout_ms", 5000)
	viper.SetDefault("webhook.retry_max", 3)
	viper.SetDefault("webhook.text_template", DefaultWebhookTextTemplate)

	viper.SetDefault("bark.enabled", false)
	viper.SetDefault("bark.group", "vohive")
	viper.SetDefault("bark.level", "active")
	viper.SetDefault("email.enabled", false)
	viper.SetDefault("email.use_ssl", false)
	viper.SetDefault("pushplus.enabled", false)
	viper.SetDefault("web.username", "admin")
	viper.SetDefault("web.password", "admin")
	viper.SetDefault("vowifi.enabled", false)
	viper.SetDefault("vowifi.mode", "vowifi")
	viper.SetDefault("imscore.use_sipgo_udp", false)

	// 官方默认推送秘钥与用户 (留空则不执行 Push)
	viper.SetDefault("vowifi.voice_gateway.linphone_push.linphone_user", "")
	viper.SetDefault("vowifi.voice_gateway.linphone_push.linphone_password", "")

	// 环境变量覆盖支持 (例如 PROXY_DEVICES_0_APN)
	viper.SetEnvPrefix("PROXY")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if cfg.FreeDeviceLimit < 0 {
		return nil, fmt.Errorf("free_device_limit 不能小于 0（0 表示无限制）")
	}

	// 兼容旧版单值配置: feishu.chat_id
	if len(cfg.Feishu.ChatIDs) == 0 && strings.TrimSpace(cfg.Feishu.ChatID) != "" {
		cfg.Feishu.ChatIDs = []string{strings.TrimSpace(cfg.Feishu.ChatID)}
	}

	// 兼容 server.port 格式 (例如: 7575 和 :7575)
	if cfg.Server.Port != "" && !strings.Contains(cfg.Server.Port, ":") {
		cfg.Server.Port = ":" + cfg.Server.Port
	}

	return &cfg, nil
}
