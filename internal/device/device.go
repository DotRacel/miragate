// Package device 实现 relay 的设备身份与 mrs-sig-v1 请求签名。
//
// 对应 Mirasim server.cjs 中的 pen / pbs / dfe / fbs / hbs 与设备会话
// (/v1/device/session) 换票逻辑（pfe 类）。
//
// 机制概述：
//   - 本机持有一把 Ed25519 私钥（PKCS8 PEM，存于配置 device.privateKey）。
//   - deviceId = base64url(sha256(SPKI-DER 公钥的 base64))[:22]。
//   - 每个发往 relay 的请求附带签名头：
//     x-mirasim-device / x-mirasim-ts / x-mirasim-nonce / x-mirasim-sig
//     其中签名串 = ["mrs-sig-v1", METHOD, path, ts, nonce, sha256hex(body)].join("\n")
//     用 Ed25519 私钥签名后取 base64url。
//   - 可选：POST /v1/device/session 用 Bearer 令牌换取短期 ticket，
//     之后用 ticket 代替原始 JWT 作为凭据（relay 不支持时回退为纯令牌）。
package device

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strconv"
	"strings"
	"time"
)

// 签名协议常量，与 server.cjs 完全一致。
const (
	SigScheme    = "mrs-sig-v1"
	HeaderDevice = "x-mirasim-device"
	HeaderTS     = "x-mirasim-ts"
	HeaderNonce  = "x-mirasim-nonce"
	HeaderSig    = "x-mirasim-sig"
	HeaderClient = "x-mirasim-client"
	SessionPath  = "/v1/device/session"
)

// Key 是一把可用于签名的设备密钥。
type Key struct {
	priv         ed25519.PrivateKey
	deviceID     string
	publicKeyB64 string // SPKI DER 的 base64
}

// LoadOrCreate 从 PEM 加载私钥；pem 为空则生成新钥并通过 save 回调持久化其 PEM。
func LoadOrCreate(pemStr string, save func(pemStr string) error) (*Key, error) {
	if strings.TrimSpace(pemStr) != "" {
		if k, err := fromPEM(pemStr); err == nil {
			return k, nil
		}
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	newPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if save != nil {
		if err := save(newPEM); err != nil {
			return nil, err
		}
	}
	return newKey(priv)
}

func fromPEM(pemStr string) (*Key, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("device: invalid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("device: not an Ed25519 key")
	}
	return newKey(priv)
}

func newKey(priv ed25519.PrivateKey) (*Key, error) {
	pub := priv.Public().(ed25519.PublicKey)
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	b64 := base64.StdEncoding.EncodeToString(spki)
	return &Key{priv: priv, publicKeyB64: b64, deviceID: deriveDeviceID(b64)}, nil
}

// deriveDeviceID 对应 pbs：base64url(sha256(publicKeyB64))[:22]。
func deriveDeviceID(publicKeyB64 string) string {
	sum := sha256.Sum256([]byte(publicKeyB64))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:22]
}

// DeviceID 返回签名用的设备 ID。
func (k *Key) DeviceID() string { return k.deviceID }

// PublicKeyB64 返回 SPKI DER 公钥的 base64（用于 /v1/device/session）。
func (k *Key) PublicKeyB64() string { return k.publicKeyB64 }

// SignRequest 计算一次请求的 mrs-sig-v1 签名头。
// method 会转大写；path 为不含 query 的路径；body 为原始请求体。
func (k *Key) SignRequest(method, path string, body []byte, clientVer string) map[string]string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonceRaw := make([]byte, 16)
	_, _ = rand.Read(nonceRaw)
	nonce := base64.RawURLEncoding.EncodeToString(nonceRaw)

	bodyHash := hex.EncodeToString(sha256Sum(body))
	signingStr := strings.Join([]string{SigScheme, strings.ToUpper(method), path, ts, nonce, bodyHash}, "\n")
	sig := ed25519.Sign(k.priv, []byte(signingStr))

	h := map[string]string{
		HeaderDevice: k.deviceID,
		HeaderTS:     ts,
		HeaderNonce:  nonce,
		HeaderSig:    base64.RawURLEncoding.EncodeToString(sig),
	}
	if clientVer != "" {
		h[HeaderClient] = clientVer
	}
	return h
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}
