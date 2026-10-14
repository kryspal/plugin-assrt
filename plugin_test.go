package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/kryspal/plugin-sdk"
	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
)

// testToken is a made-up token of the right shape.
const testKey = "AbCdEf0123456789aBcDeF0123456789"

var han = regexp.MustCompile(`\p{Han}`)

func TestDescribe(t *testing.T) {
	p := newPlugin()
	for _, lang := range []string{"en", "zh-TW", "zh-CN", "", "ja"} {
		m, err := p.Describe(context.Background(), &pluginv1.DescribeRequest{Language: lang})
		if err != nil {
			t.Fatal(err)
		}
		if m.GetId() != "assrt" || m.GetName() != "ASSRT" || m.GetDescription() == "" || m.GetVersion() != version() ||
			!slices.Equal(m.GetCapabilities(), []string{pluginv1.CapabilitySubtitlesSearch}) ||
			len(m.GetFeatures()) != 0 || m.GetSuggestedConcurrency() != 1 || len(m.GetConfigFields()) != 2 {
			t.Errorf("Describe(%q) = %v", lang, m)
		}
		if !m.GetConfigFields()[0].GetRequired() || m.GetConfigFields()[0].GetType() != pluginv1.ConfigFieldType_CONFIG_FIELD_TYPE_SECRET {
			t.Errorf("the token must be a required secret: %v", m.GetConfigFields()[0])
		}
		if m.GetAttribution().GetText() != attributionText || m.GetAttribution().GetLink() != "https://assrt.net" {
			t.Errorf("attribution = %v", m.GetAttribution())
		}
		if (lang == "en" || lang == "" || lang == "ja") && han.MatchString(m.GetDescription()) {
			t.Errorf("Describe(%q): Han characters in the English text", lang)
		}
	}
	for _, tag := range messages.Tags() {
		for key := range messageTables["en"] {
			if messages.For(tag).T(key) == "" {
				t.Errorf("%s: missing %s", tag, key)
			}
		}
	}
	if strings.ContainsAny(messageTables["zh-TW"]["desc"], "么识断读网为") {
		t.Error("the zh-TW text has simplified characters")
	}
	if strings.ContainsAny(messageTables["zh-CN"]["desc"], "麼識斷讀網為") {
		t.Error("the zh-CN text has traditional characters")
	}
}

func TestConfigure(t *testing.T) {
	p := newPlugin()
	for name, c := range map[string]struct {
		values map[string]string
		field  string // the field expected to error; empty means it passes
	}{
		"valid token":      {map[string]string{keyToken: testKey}, ""},
		"upper case":       {map[string]string{keyToken: strings.ToUpper(testKey)}, ""},
		"empty":            {map[string]string{}, keyToken},
		"short token":      {map[string]string{keyToken: "abc123"}, keyToken},
		"not alphanumeric": {map[string]string{keyToken: strings.Repeat("g", 31) + "-"}, keyToken},
		"http base url":    {map[string]string{keyToken: testKey, keyBaseURL: "http://proxy.example"}, keyBaseURL},
		"https base url":   {map[string]string{keyToken: testKey, keyBaseURL: "https://proxy.example/v1/"}, ""},
	} {
		resp, err := p.Configure(context.Background(), &pluginv1.ConfigureRequest{Values: c.values})
		if err != nil {
			t.Fatal(err)
		}
		if c.field == "" && len(resp.GetFieldErrors()) != 0 || c.field != "" && resp.GetFieldErrors()[c.field] == "" {
			t.Errorf("%s: field errors = %v, want %q", name, resp.GetFieldErrors(), c.field)
		}
	}
	_, _ = p.Configure(context.Background(), &pluginv1.ConfigureRequest{Values: map[string]string{keyToken: " " + testKey + " ", keyBaseURL: "https://proxy.example/v1/"}})
	if k, b := p.settings(); k != testKey || b != "https://proxy.example/v1" {
		t.Errorf("settings = %q %q", k, b)
	}
}

// TestPluginProcess is the conformance check of the plugin process (the host's way): it compiles the executable and starts it with the host's environment
// variables, checking the handshake, health check, Describe, Search (no token is needs-configuration, no network) and graceful exit after SIGTERM.
func TestPluginProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the plugin binary")
	}
	exe := filepath.Join(t.TempDir(), "assrt")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	sockDir, err := os.MkdirTemp("", "ass-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(sockDir) }()
	cmd := exec.Command(exe)
	cmd.Env = []string{pluginapi.EnvCookie + "=" + pluginapi.CookieValue, pluginapi.EnvProtocols + "=1", pluginapi.EnvSocketDir + "=" + sockDir, "HOME=" + t.TempDir()}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	hs, err := pluginapi.ParseHandshake(strings.TrimSpace(line))
	if err != nil || hs.Protocol != 1 || hs.Network != pluginapi.NetworkUnix {
		t.Fatalf("handshake %q: %+v, %v", line, hs, err)
	}
	conn, err := grpc.NewClient("unix:"+hs.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hc := healthpb.NewHealthClient(conn)
	for _, svc := range []string{"", pluginv1.HealthServiceSubtitleProvider} {
		if r, err := hc.Check(ctx, &healthpb.HealthCheckRequest{Service: svc}); err != nil || r.GetStatus() != healthpb.HealthCheckResponse_SERVING {
			t.Errorf("health %q = %v, %v", svc, r, err)
		}
	}
	pc := pluginv1.NewPluginClient(conn)
	m, err := pc.Describe(ctx, &pluginv1.DescribeRequest{Language: "zh-TW"})
	if err != nil || m.GetId() != "assrt" || m.GetProtocolVersion() != pluginv1.ProtocolVersion {
		t.Fatalf("Describe = %v, %v", m, err)
	}
	// Without a token the plugin needs configuration, and no network request is made.
	if _, err := pluginv1.NewSubtitleProviderClient(conn).Search(ctx, &pluginv1.SearchSubtitlesRequest{}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("Search without a token = %v, want FAILED_PRECONDITION", err)
	}
	if _, err := pluginv1.NewMetadataProviderClient(conn).Search(ctx, &pluginv1.SearchRequest{}); err == nil {
		t.Error("the metadata service must not be registered")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	stopped = true
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit after SIGTERM: %v", err)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Error("the plugin did not exit after SIGTERM")
	}
}
