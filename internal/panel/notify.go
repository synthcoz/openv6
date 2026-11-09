package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Alert kinds (each can be switched off in settings: notify_<kind>=0).
const (
	AlertNodeOffline = "node_offline"
	AlertProxyFail   = "proxy_fail"
	AlertPrefix      = "prefix_change"
	AlertUpdate      = "update"
)

var alertText = map[string]map[string]string{
	"vi": {
		"node_offline":  "🔴 Node %q mất kết nối với panel",
		"node_online":   "🟢 Node %q đã kết nối lại",
		"proxy_fail":    "🔴 Proxy %s lỗi: %s",
		"proxy_ok":      "🟢 Proxy %s hoạt động lại (%s)",
		"proxy_fail_n":  "🔴 %d proxy đang lỗi trên node %q. Ví dụ: %s",
		"prefix_change": "🟡 Node %q: nhà mạng đổi prefix IPv6 %s → %s. Proxy đã tự cập nhật IP mới, port và mật khẩu giữ nguyên.",
		"update":        "⬆️ Có phiên bản openv6 mới: %s (đang dùng %s). Mở panel → Cài đặt để cập nhật.",
		"test":          "✅ openv6: kết nối Telegram thành công.",
	},
	"en": {
		"node_offline":  "🔴 Node %q lost connection to the panel",
		"node_online":   "🟢 Node %q is back online",
		"proxy_fail":    "🔴 Proxy %s failed: %s",
		"proxy_ok":      "🟢 Proxy %s works again (%s)",
		"proxy_fail_n":  "🔴 %d proxies failing on node %q. e.g. %s",
		"prefix_change": "🟡 Node %q: ISP changed the IPv6 prefix %s → %s. Proxies were re-addressed; ports and passwords are unchanged.",
		"update":        "⬆️ New openv6 version available: %s (running %s). Open the panel → Settings to update.",
		"test":          "✅ openv6: Telegram connection works.",
	},
}

func (s *Server) lang() string {
	if l := s.Store.GetSetting("lang", "vi"); l == "en" {
		return "en"
	}
	return "vi"
}

func (s *Server) msg(key string, args ...any) string {
	t := alertText[s.lang()][key]
	if t == "" {
		t = alertText["en"][key]
	}
	return fmt.Sprintf(t, args...)
}

// alert sends a Telegram message if configured and the alert kind is enabled.
func (s *Server) alert(kind, text string) {
	if !s.pro() { // Telegram alerts are a Pro feature
		return
	}
	if s.Store.GetSetting("notify_"+kind, "1") != "1" {
		return
	}
	go func() {
		if err := s.sendTelegram(context.Background(), text); err != nil && err != errTelegramOff {
			log.Printf("telegram: %v", err)
		}
	}()
}

var errTelegramOff = fmt.Errorf("telegram not configured")

func (s *Server) sendTelegram(ctx context.Context, text string) error {
	tok := strings.TrimSpace(s.Store.GetSetting("telegram_token", ""))
	chat := strings.TrimSpace(s.Store.GetSetting("telegram_chat", ""))
	if tok == "" || chat == "" {
		return errTelegramOff
	}
	name := s.Store.GetSetting("instance_name", "")
	if name != "" {
		text = "[" + name + "] " + text
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	form := url.Values{"chat_id": {chat}, "text": {text}, "disable_web_page_preview": {"true"}}
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+tok+"/sendMessage", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		// do not leak the token through the URL in the error
		return fmt.Errorf("cannot reach api.telegram.org")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var j struct {
			Description string `json:"description"`
		}
		_ = json.NewDecoder(res.Body).Decode(&j)
		return fmt.Errorf("telegram: %s %s", res.Status, j.Description)
	}
	return nil
}

func (s *Server) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	if err := s.sendTelegram(r.Context(), s.msg("test")); err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
