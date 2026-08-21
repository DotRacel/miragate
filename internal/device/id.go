package device

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var (
	idMu   sync.Mutex
	idFile = "device.json"
)

// PersistentID 返回稳定的设备 UUID（用于 x-mirasim-device 元信息头），
// 存于 <home>/device.json，对应 server.cjs 的 bM()。缺失则生成并落盘。
func PersistentID(home string) string {
	idMu.Lock()
	defer idMu.Unlock()
	path := filepath.Join(home, idFile)
	if data, err := os.ReadFile(path); err == nil {
		var d struct {
			DeviceID string `json:"deviceId"`
		}
		if json.Unmarshal(data, &d) == nil && d.DeviceID != "" {
			return d.DeviceID
		}
	}
	id := newUUID()
	_ = os.MkdirAll(home, 0o700)
	if data, err := json.Marshal(map[string]string{"deviceId": id}); err == nil {
		_ = os.WriteFile(path, append(data, '\n'), 0o600)
	}
	return id
}

// newUUID 生成 RFC4122 v4 UUID。
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}
