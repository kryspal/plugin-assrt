package main

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kryspal/plugin-sdk/plugintest"
	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
)

// TestConformance builds the plugin and runs the SDK conformance suite against the binary, with a fake ASSRT over HTTPS (the plugin
// only accepts an https base URL, and trusts the fake's certificate through SSL_CERT_FILE).
func TestConformance(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "plugin")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	up := plugintest.NewTLSUpstream(t)
	up.Handle("GET /sub/search", plugintest.JSON(http.StatusOK, map[string]any{"status": 0, "sub": map[string]any{"subs": []any{map[string]any{
		"id": 601001, "native_name": "Sample Movie", "subtype": "Subrip(srt)", "videoname": "Sample.Movie.2020.1080p.mkv",
		"lang": map[string]any{"langlist": map[string]bool{"langeng": true}, "desc": "en"},
	}}}}))
	plugintest.Run(t, bin, plugintest.Options{
		Config:    map[string]string{"token": "0123456789abcdef0123456789abcdef", "base_url": up.URL},
		Env:       up.Env(),
		Subtitles: &plugintest.SubtitleCase{Type: pluginv1.ItemType_ITEM_TYPE_MOVIE, Title: "Sample Movie", Year: 2020},
	})
}
