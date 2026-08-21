package device

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// TestDeviceIDDerivation 验证 deviceId = base64url(sha256(pubKeyB64))[:22]（对应 pbs）。
func TestDeviceIDDerivation(t *testing.T) {
	k, err := LoadOrCreate("", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := base64.RawURLEncoding.EncodeToString(sum(k.PublicKeyB64()))[:22]
	if k.DeviceID() != want {
		t.Fatalf("deviceID=%q want %q", k.DeviceID(), want)
	}
	if len(k.DeviceID()) != 22 {
		t.Fatalf("deviceID len=%d want 22", len(k.DeviceID()))
	}
}

// TestSignRequestVerifies 验证 mrs-sig-v1 签名串格式且可用公钥校验通过。
func TestSignRequestVerifies(t *testing.T) {
	k, err := LoadOrCreate("", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"model":"claude-sonnet-5","max_tokens":1}`)
	h := k.SignRequest("post", "/v1/messages", body, "0.0.214")

	for _, key := range []string{HeaderDevice, HeaderTS, HeaderNonce, HeaderSig, HeaderClient} {
		if h[key] == "" {
			t.Fatalf("missing header %s", key)
		}
	}
	if h[HeaderDevice] != k.DeviceID() {
		t.Fatalf("device header mismatch")
	}

	// 复算签名串：scheme\nMETHOD(大写)\npath\nts\nnonce\nsha256hex(body)
	bodyHash := hex.EncodeToString(sum2(body))
	signingStr := strings.Join([]string{SigScheme, "POST", "/v1/messages", h[HeaderTS], h[HeaderNonce], bodyHash}, "\n")

	sig, err := base64.RawURLEncoding.DecodeString(h[HeaderSig])
	if err != nil {
		t.Fatalf("sig not base64url: %v", err)
	}
	pub := recoverPub(t, k.PublicKeyB64())
	if !ed25519.Verify(pub, []byte(signingStr), sig) {
		t.Fatal("签名校验失败：签名串格式与 mrs-sig-v1 不一致")
	}
}

// TestSignEmptyBody 验证空 body 时用 sha256("") 的十六进制。
func TestSignEmptyBody(t *testing.T) {
	k, _ := LoadOrCreate("", func(string) error { return nil })
	h := k.SignRequest("GET", "/v1/limits", nil, "")
	if _, ok := h[HeaderClient]; ok {
		t.Fatal("clientVer 为空时不应有 client 头")
	}
	emptyHash := hex.EncodeToString(sum2(nil))
	signingStr := strings.Join([]string{SigScheme, "GET", "/v1/limits", h[HeaderTS], h[HeaderNonce], emptyHash}, "\n")
	sig, _ := base64.RawURLEncoding.DecodeString(h[HeaderSig])
	if !ed25519.Verify(recoverPub(t, k.PublicKeyB64()), []byte(signingStr), sig) {
		t.Fatal("空 body 签名校验失败")
	}
}

// TestKeyRoundTrip 验证私钥 PEM 持久化后可重新加载且 deviceId 稳定。
func TestKeyRoundTrip(t *testing.T) {
	var saved string
	k1, _ := LoadOrCreate("", func(pem string) error { saved = pem; return nil })
	if saved == "" {
		t.Fatal("未回调保存 PEM")
	}
	k2, err := LoadOrCreate(saved, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if k1.DeviceID() != k2.DeviceID() {
		t.Fatal("重新加载后 deviceId 变化")
	}
}

func sum(s string) []byte  { h := sha256.Sum256([]byte(s)); return h[:] }
func sum2(b []byte) []byte { h := sha256.Sum256(b); return h[:] }

func recoverPub(t *testing.T, b64 string) ed25519.PublicKey {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	p, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatal(err)
	}
	return p.(ed25519.PublicKey)
}
