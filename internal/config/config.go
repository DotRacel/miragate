// Package config 负责 miragate 的持久化配置读写。
//
// 配置模型与 Mirasim 桌面端保持兼容：JWT access_token / refresh_token 与
// auth.mirasim.ai 完全互通，设备私钥用于 relay 的 mrs-sig-v1 请求签名。
//
// 默认配置目录为 ~/.miragate（可用 MIRAGATE_HOME 覆盖）。为便于直接复用
// Mirasim 桌面端已登录的凭据，若本地不存在配置且 ~/.mirasim 下存在
// setting.json/config.json，会尝试导入其中的 auth 字段。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Auth 保存一次登录得到的凭据与身份缓存。
type Auth struct {
	Token        string `json:"token"`                  // JWT access_token
	RefreshToken string `json:"refreshToken,omitempty"` // 刷新令牌
	UserID       string `json:"userId,omitempty"`       // JWT sub
	Exp          int64  `json:"exp,omitempty"`          // JWT exp（秒）
	Name         string `json:"name,omitempty"`         // 显示名缓存
	Email        string `json:"email,omitempty"`        // 邮箱缓存
}

// Device 保存本机设备私钥（Ed25519 PKCS8 PEM），用于 relay 请求签名。
type Device struct {
	PrivateKey string `json:"privateKey,omitempty"`
}

// Config 是落盘的完整配置。
type Config struct {
	Auth   *Auth   `json:"auth,omitempty"`
	Device *Device `json:"device,omitempty"`

	// Listen 是本地反代监听地址（默认 127.0.0.1:8788）。
	Listen string `json:"listen,omitempty"`

	// AuthURL / RelayURL 覆盖上游地址，留空则用内置默认值。
	AuthURL  string `json:"authUrl,omitempty"`
	RelayURL string `json:"relayUrl,omitempty"`
}

const (
	// DefaultAuthURL 是 Mirasim 的鉴权服务。
	DefaultAuthURL = "https://auth.mirasim.ai"
	// DefaultRelayURL 是 Mirasim 的 relay/网关服务。
	DefaultRelayURL = "https://relay.mirasim.ai"
	// DefaultListen 是本地反代默认监听地址。
	DefaultListen = "127.0.0.1:8788"
)

var (
	mu     sync.Mutex
	cached *Config
)

// Home 返回 miragate 配置目录。
func Home() string {
	if v := os.Getenv("MIRAGATE_HOME"); v != "" {
		return v
	}
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ".miragate"
	}
	return filepath.Join(h, ".miragate")
}

// Path 返回主配置文件路径。
func Path() string {
	return filepath.Join(Home(), "config.json")
}

// AuthBaseURL 解析生效的鉴权服务地址（配置 > 环境变量 > 默认）。
func (c *Config) AuthBaseURL() string {
	if c != nil && c.AuthURL != "" {
		return c.AuthURL
	}
	if v := os.Getenv("MIRAGATE_AUTH_URL"); v != "" {
		return v
	}
	if v := os.Getenv("LOGIN_URL"); v != "" {
		return v
	}
	return DefaultAuthURL
}

// RelayBaseURL 解析生效的 relay 服务地址。
func (c *Config) RelayBaseURL() string {
	if c != nil && c.RelayURL != "" {
		return c.RelayURL
	}
	for _, k := range []string{"MIRAGATE_RELAY_URL", "RELAY_BASE_URL", "RELAY_URL", "BACKEND_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return DefaultRelayURL
}

// ListenAddr 返回本地反代监听地址。
func (c *Config) ListenAddr() string {
	if c != nil && c.Listen != "" {
		return c.Listen
	}
	if v := os.Getenv("MIRAGATE_LISTEN"); v != "" {
		return v
	}
	return DefaultListen
}

// Load 读取配置；若不存在则返回空配置并尝试从 Mirasim 导入凭据。
func Load() (*Config, error) {
	mu.Lock()
	defer mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	c := &Config{}
	data, err := os.ReadFile(Path())
	switch {
	case err == nil:
		if e := json.Unmarshal(data, c); e != nil {
			return nil, e
		}
	case os.IsNotExist(err):
		importFromMirasim(c)
	default:
		return nil, err
	}
	cached = c
	return c, nil
}

// Save 原子写回配置（权限 0600）。
func Save(c *Config) error {
	mu.Lock()
	defer mu.Unlock()
	cached = c
	dir := Home()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := Path() + "." + itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// importFromMirasim 在首次运行时尝试复用桌面端已有登录态。
func importFromMirasim(c *Config) {
	h, err := os.UserHomeDir()
	if err != nil {
		return
	}
	base := os.Getenv("HOME")
	if base == "" {
		base = filepath.Join(h, ".mirasim")
	}
	for _, name := range []string{"config.json", "setting.json"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			continue
		}
		var src Config
		if json.Unmarshal(data, &src) != nil {
			continue
		}
		// 只导入明文（未加密，mrs1: 前缀的密文无法在此解密）凭据。
		if src.Auth != nil && src.Auth.Token != "" && !isEncrypted(src.Auth.Token) {
			c.Auth = src.Auth
		}
		if src.Device != nil && src.Device.PrivateKey != "" && !isEncrypted(src.Device.PrivateKey) {
			c.Device = src.Device
		}
		if c.Auth != nil {
			return
		}
	}
}

// isEncrypted 判断值是否为 Mirasim 的 keychain 密文（mrs1: 前缀）。
func isEncrypted(s string) bool {
	return len(s) >= 5 && s[:5] == "mrs1:"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
