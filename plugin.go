package main

import (
	"context"
	_ "embed"
	"net/url"
	"regexp"
	"strings"
	"sync"

	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
)

const (
	pluginID = "assrt"

	keyToken   = "token"
	keyBaseURL = "base_url"

	defaultBaseURL = "https://api.assrt.net/v1"
	// attributionText is the credit the ASSRT terms ask for, as an English original (the same in every language); shown in the source line on the plugin page.
	attributionText = "Subtitles provided by assrt.net"
	attributionLink = "https://assrt.net"
)

// tokenRE is the shape of an ASSRT token: 32 letters and digits.
var tokenRE = regexp.MustCompile(`^[0-9A-Za-z]{32}$`)

// versionFile is the single source of the plugin version: the manifest's version, and also the <version> of the bundled directory (change request 0017).
//
//go:embed VERSION
var versionFile string

func version() string { return strings.TrimSpace(versionFile) }

type plugin struct {
	pluginv1.UnimplementedPluginServer
	pluginv1.UnimplementedSubtitleProviderServer

	mu      sync.Mutex
	token   string
	baseURL string
	api     *api // replaceable in tests
}

func newPlugin() *plugin {
	return &plugin{baseURL: defaultBaseURL, api: newAPI()}
}

// Describe declares subtitles.search. The token is required: without it the plugin is in the needs-configuration state.
func (p *plugin) Describe(_ context.Context, req *pluginv1.DescribeRequest) (*pluginv1.Manifest, error) {
	t := messages.For(req.GetLanguage())
	return &pluginv1.Manifest{
		Id: pluginID, Name: "ASSRT", Description: t.T("desc"), Version: version(), ProtocolVersion: pluginv1.ProtocolVersion,
		Capabilities: []string{pluginv1.CapabilitySubtitlesSearch},
		ConfigGroups: []*pluginv1.ConfigGroup{{Key: "advanced", Title: t.T("groupAdvanced")}},
		ConfigFields: []*pluginv1.ConfigField{
			{Key: keyToken, Type: pluginv1.ConfigFieldType_CONFIG_FIELD_TYPE_SECRET, Required: true, Label: t.T("tokenLabel"),
				Description: t.T("tokenHelp"), Link: "https://secure.assrt.net/user/", LinkLabel: t.T("tokenLink")},
			{Key: keyBaseURL, Type: pluginv1.ConfigFieldType_CONFIG_FIELD_TYPE_STRING, Group: "advanced", Label: t.T("baseURLLabel"),
				Description: t.T("baseURLHelp"), Placeholder: defaultBaseURL},
		},
		Attribution:          &pluginv1.Attribution{Text: attributionText, Link: attributionLink},
		SuggestedConcurrency: 1,
	}, nil
}

// Configure validates the settings: the token is required and must be 32 letters and digits; the URL must be https. It does not verify the token
// over the network (a rejected token is reported by the first call as needing configuration).
func (p *plugin) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	t := messages.For(req.GetLanguage())
	v := req.GetValues()
	resp := &pluginv1.ConfigureResponse{FieldErrors: map[string]string{}}
	token := strings.TrimSpace(v[keyToken])
	switch {
	case token == "":
		resp.FieldErrors[keyToken] = t.T("errRequired")
	case !tokenRE.MatchString(token):
		resp.FieldErrors[keyToken] = t.T("errTokenFormat")
	}
	base := strings.TrimSpace(v[keyBaseURL])
	if base == "" {
		base = defaultBaseURL
	}
	if u, err := url.Parse(base); err != nil || u.Scheme != "https" || u.Host == "" {
		resp.FieldErrors[keyBaseURL] = t.T("errHTTPS")
	}
	if len(resp.FieldErrors) > 0 {
		return resp, nil
	}
	p.mu.Lock()
	p.token, p.baseURL = token, strings.TrimRight(base, "/")
	p.mu.Unlock()
	return resp, nil
}

func (p *plugin) settings() (token, base string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.token, p.baseURL
}

// forgetToken clears the token after ASSRT rejected it, unless a new one was saved in the meantime.
func (p *plugin) forgetToken(rejected string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token == rejected {
		p.token = ""
	}
}
