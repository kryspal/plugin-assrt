package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
	"github.com/kryspal/plugin-sdk/sdk"
)

// A recorded-shape search response (made-up names and ids).
const searchBody = `{"status":0,"sub":{"action":"search","result":"succeed","keyword":"x","subs":[
 {"id":601001,"native_name":"测试电影","revision":0,"upload_time":"2020-01-01 10:00:00","subtype":"Subrip(srt)","vote_score":0,
  "videoname":"Made.Up.Movie.2019.1080p.BluRay.x264-GRP.mkv","lang":{"langlist":{"langchs":true,"langcht":true,"langeng":true},"desc":"简 繁 英"}},
 {"id":601002,"native_name":"测试电影","subtype":"ASS/SSA(ass)","videoname":"Other.Release.mkv","lang":{"langlist":{"langeng":true},"desc":"英"}},
 {"id":601003,"native_name":"测试电影","subtype":"VobSub(sub)","videoname":"","lang":{"desc":"繁"}},
 {"id":601004,"native_name":"测试电影","subtype":"","videoname":"","lang":{}}
]}}`

type fake struct {
	*httptest.Server
	mu     sync.Mutex
	hits   []string // request URIs
	auth   []string // Authorization headers of the API requests
	routes map[string]func(w http.ResponseWriter, r *http.Request)
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.URL.RequestURI())
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			f.auth = append(f.auth, r.Header.Get("Authorization"))
		}
		h := f.routes[r.URL.Path]
		f.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fake) json(path string, code int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

func (f *fake) file(path string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }
}

func (f *fake) count(prefix string) (n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.hits {
		if strings.HasPrefix(h, prefix) {
			n++
		}
	}
	return n
}

func configured(t *testing.T, f *fake) *plugin {
	t.Helper()
	p := newPlugin()
	p.api = newAPI()
	p.api.http = f.Client()
	p.api.interval = 0
	resp, err := p.Configure(context.Background(), &pluginv1.ConfigureRequest{Values: map[string]string{keyToken: testKey, keyBaseURL: f.URL + "/v1"}})
	if err != nil || len(resp.GetFieldErrors()) != 0 {
		t.Fatalf("Configure = %v, %v", resp, err)
	}
	return p
}

func movieTarget() *pluginv1.SubtitleTarget {
	return &pluginv1.SubtitleTarget{Type: pluginv1.ItemType_ITEM_TYPE_MOVIE, Title: "Made Up Movie", Year: 2019, FileName: "Made.Up.Movie.2019.1080p.BluRay.x264-GRP.mkv"}
}

func ids(r *pluginv1.SearchSubtitlesResponse) string {
	var s []string
	for _, c := range r.GetCandidates() {
		s = append(s, c.GetId()+":"+c.GetLanguage())
	}
	return strings.Join(s, ",")
}

func TestSearchByFileNameThenConversion(t *testing.T) {
	f := newFake(t)
	f.json("/v1/sub/search", 200, searchBody)
	p := configured(t, f)
	resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: movieTarget(), Languages: []string{"zh-TW", "en"}})
	if err != nil {
		t.Fatal(err)
	}
	// Wanted zh-TW then en: 601001 has traditional (chosen), 601002 only English, 601003 has traditional by its description, 601004 has no language.
	if got := ids(resp); got != "601001:zh-TW,601002:en,601003:zh-TW" {
		t.Errorf("candidates = %s", got)
	}
	c := resp.GetCandidates()[0]
	if c.GetFormat() != "srt" || c.GetMatchedBy() != pluginv1.SubtitleCandidate_MATCHED_BY_NAME || c.GetScore() != 1 ||
		c.GetReleaseName() != "Made.Up.Movie.2019.1080p.BluRay.x264-GRP.mkv" {
		t.Errorf("first candidate = %v", c)
	}
	if got := resp.GetCandidates()[1].GetFormat(); got != "ass" {
		t.Errorf("format of the ASS/SSA(ass) subtype = %q", got)
	}
	if got := resp.GetCandidates()[2]; got.GetFormat() != "sub" || got.GetReleaseName() != "测试电影" {
		t.Errorf("a candidate with no video name uses the native name: %v", got)
	}
	// The first query is the file name without its extension, with is_file and no_muxer; the token is in the header, not the URL.
	u, _ := url.Parse(f.hits[0])
	q := u.Query()
	if u.Path != "/v1/sub/search" || q.Get("q") != "Made.Up.Movie.2019.1080p.BluRay.x264-GRP" || q.Get("is_file") != "1" || q.Get("no_muxer") != "1" || strings.Contains(f.hits[0], testKey) {
		t.Errorf("request = %s", f.hits[0])
	}
	if f.auth[0] != "Bearer "+testKey {
		t.Errorf("Authorization = %q", f.auth[0])
	}
	if f.count("/v1/sub/search") != 1 {
		t.Errorf("%d searches; results from the file name must not trigger a second query", f.count("/v1/sub/search"))
	}
}

func TestSearchFallsBackToTheTitle(t *testing.T) {
	f := newFake(t)
	f.routes["/v1/sub/search"] = func(w http.ResponseWriter, r *http.Request) {
		body := `{"status":0,"sub":{"subs":[]}}`
		if r.URL.Query().Get("is_file") == "" {
			body = searchBody
		}
		_, _ = w.Write([]byte(body))
	}
	p := configured(t, f)
	resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: movieTarget(), Languages: []string{"zh-CN"}})
	if err != nil || ids(resp) != "601001:zh-CN" {
		t.Fatalf("Search = %s, %v", ids(resp), err)
	}
	if resp.GetCandidates()[0].GetScore() != 0.8 && resp.GetCandidates()[0].GetScore() != 1 {
		t.Errorf("score = %v", resp.GetCandidates()[0].GetScore())
	}
	u, _ := url.Parse(f.hits[1])
	if got := u.Query().Get("q"); got != "Made Up Movie 2019" || u.Query().Get("is_file") != "" {
		t.Errorf("second query = %q (is_file %q)", got, u.Query().Get("is_file"))
	}
}

func TestEpisodeQueryAndLimit(t *testing.T) {
	f := newFake(t)
	f.json("/v1/sub/search", 200, searchBody)
	p := configured(t, f)
	s, e := int32(2), int32(5)
	target := &pluginv1.SubtitleTarget{Type: pluginv1.ItemType_ITEM_TYPE_EPISODE, Title: "Made Up Show", SeasonNumber: &s, EpisodeNumber: &e, FileName: "ab.mkv"}
	resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: target, Limit: 2})
	if err != nil || len(resp.GetCandidates()) != 2 {
		t.Fatalf("Search = %v, %v; want the limit of 2 applied", resp, err)
	}
	// "ab" is shorter than three characters: only the title query is sent.
	if len(f.hits) != 1 || !strings.Contains(f.hits[0], "q=Made+Up+Show+S02E05") {
		t.Errorf("requests = %v", f.hits)
	}
	// No wanted language: each candidate gets its first language (601004 has none and is kept without one).
	if got := ids(resp); got != "601001:zh-CN,601002:en" {
		t.Errorf("candidates = %s", got)
	}
}

func TestSearchWithNothingToAsk(t *testing.T) {
	f := newFake(t)
	p := configured(t, f)
	resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: &pluginv1.SubtitleTarget{Type: pluginv1.ItemType_ITEM_TYPE_MOVIE}})
	if err != nil || len(resp.GetCandidates()) != 0 || len(f.hits) != 0 {
		t.Errorf("%v, %v, %d requests", resp, err, len(f.hits))
	}
}

func TestLanguageMatching(t *testing.T) {
	for _, c := range []struct {
		have, want string
		ok         bool
	}{
		{"zh-TW", "zh-TW", true}, {"zh-TW", "zh-Hant", true}, {"zh-TW", "zh-HK", true}, {"zh-CN", "zh-Hant", false}, {"zh-CN", "zh-Hans", true},
		{"zh-CN", "zh", true}, {"zh-TW", "zh", true}, {"zh-CN", "zh-TW", false}, {"en", "en", true}, {"en", "en-US", false}, {"ja", "ko", false}, {"en", "zh", false},
	} {
		if got := sameLanguage(c.have, c.want); got != c.ok {
			t.Errorf("sameLanguage(%q, %q) = %v, want %v", c.have, c.want, got, c.ok)
		}
	}
}

func TestStatuses(t *testing.T) {
	for _, c := range []struct {
		name string
		http int
		body string
		hdr  map[string]string
		code codes.Code // gRPC code the plugin's error maps to; OK means an empty list
		rate time.Duration
	}{
		{"keyword too short", 200, `{"status":101}`, nil, codes.OK, 0},
		{"no such subtitle", 404, `{"status":20900}`, nil, codes.OK, 0},
		{"over quota", 500, `{"status":30900}`, map[string]string{"Retry-After": "45"}, codes.Unknown, 45 * time.Second},
		{"http 429", 429, `{}`, nil, codes.Unknown, time.Minute},
		{"server error", 500, `{"status":30000}`, nil, codes.Unavailable, 0},
		{"not json", 502, `<html>`, nil, codes.Unavailable, 0},
		{"refused", 400, `{"status":20000}`, nil, codes.InvalidArgument, 0},
		{"bad token", 403, `{"status":20001}`, nil, codes.FailedPrecondition, 0},
		{"no such user", 200, `{"status":1}`, nil, codes.FailedPrecondition, 0},
	} {
		f := newFake(t)
		f.routes["/v1/sub/search"] = func(w http.ResponseWriter, r *http.Request) {
			for k, v := range c.hdr {
				w.Header().Set(k, v)
			}
			w.WriteHeader(c.http)
			_, _ = w.Write([]byte(c.body))
		}
		p := configured(t, f)
		resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: movieTarget()})
		if c.rate > 0 {
			if rl, ok := errors.AsType[*sdk.RateLimitError](err); !ok || rl.RetryAfter != c.rate {
				t.Errorf("%s: %v, want a rate limit of %v", c.name, err, c.rate)
			}
			continue
		}
		if status.Code(err) != c.code {
			t.Errorf("%s: code %v (%v), want %v", c.name, status.Code(err), err, c.code)
		}
		if err == nil && len(resp.GetCandidates()) != 0 {
			t.Errorf("%s: %v", c.name, resp)
		}
		if c.code == codes.FailedPrecondition {
			// The rejected token is forgotten: the next call needs configuration without a request.
			n := len(f.hits)
			if _, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: movieTarget()}); status.Code(err) != codes.FailedPrecondition || len(f.hits) != n {
				t.Errorf("%s: after a rejected token: %v, %d new requests", c.name, err, len(f.hits)-n)
			}
		}
	}
}

func TestQuotaWindowIsRespected(t *testing.T) {
	f := newFake(t)
	f.json("/v1/sub/search", 200, `{"status":0,"sub":{"subs":[]}}`)
	p := configured(t, f)
	p.api.perMin = 3
	target := &pluginv1.SubtitleTarget{Type: pluginv1.ItemType_ITEM_TYPE_MOVIE, Title: "Made Up Movie"}
	var last error
	for range 5 {
		_, last = p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: target})
	}
	rl, ok := errors.AsType[*sdk.RateLimitError](last)
	if !ok || rl.RetryAfter <= 0 || rl.RetryAfter > time.Minute || f.count("/v1/sub/search") != 3 {
		t.Errorf("last error %v, %d requests; want a rate limit after 3 requests", last, f.count("/v1/sub/search"))
	}
}

func TestRequestsAreSpaced(t *testing.T) {
	f := newFake(t)
	f.json("/v1/sub/search", 200, `{"status":0,"sub":{"subs":[]}}`)
	p := configured(t, f)
	p.api.interval = 80 * time.Millisecond
	start := time.Now()
	done := make(chan struct{})
	for range 3 {
		go func() {
			_, _ = p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: &pluginv1.SubtitleTarget{Title: "Made Up Movie"}})
			done <- struct{}{}
		}()
	}
	for range 3 {
		<-done
	}
	if time.Since(start) < 160*time.Millisecond {
		t.Errorf("3 requests took %v", time.Since(start))
	}
}

// ---- Download ----

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func detail(f *fake, body string) {
	f.json("/v1/sub/detail", 200, strings.ReplaceAll(body, "BASE", f.URL))
}

func TestDownloadFileList(t *testing.T) {
	f := newFake(t)
	detail(f, `{"status":0,"sub":{"subs":[{"id":601001,"filename":"pack.rar","url":"BASE/dl/pack.rar","filelist":[
		{"f":"sub/Movie.chs.srt","url":"BASE/dl/a","s":"1KB"},{"f":"Movie.cht.ass","url":"BASE/dl/b","s":"1KB"},{"f":"readme.txt","url":"BASE/dl/c","s":"1KB"},{"f":"Movie.idx","url":"BASE/dl/d","s":"1KB"},
		{"f":"Movie.eng.srt","url":"ftp://files.example/e","s":"1KB"},{"f":"Movie.bin.srt","url":"BASE/dl/big","s":"3MB"}]}]}}`)
	f.file("/dl/a", []byte("simplified"))
	f.file("/dl/b", []byte("traditional"))
	f.file("/dl/big", make([]byte, maxFileBytes+10))
	p := configured(t, f)
	resp, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "601001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetFiles()) != 2 || resp.GetQuotaRemaining() != -1 {
		t.Fatalf("files = %v, quota %d; want the two text files (the txt, idx, ftp and oversize ones left out)", resp.GetFiles(), resp.GetQuotaRemaining())
	}
	a, b := resp.GetFiles()[0], resp.GetFiles()[1]
	if a.GetName() != "Movie.chs.srt" || a.GetLanguage() != "zh-CN" || string(a.GetData()) != "simplified" || b.GetName() != "Movie.cht.ass" || b.GetLanguage() != "zh-TW" {
		t.Errorf("files = %v", resp.GetFiles())
	}
	// The detail request carries the token in the header; the file requests do not carry it at all.
	for _, h := range f.hits {
		if strings.Contains(h, testKey) {
			t.Errorf("the token is in a URL: %s", h)
		}
	}
	if len(f.auth) != 1 {
		t.Errorf("%d API requests carried a token, want only the detail request", len(f.auth))
	}
}

// ASSRT sends http:// links and an empty file list as {} (both seen on the real service).
func TestDownloadTolerantShapes(t *testing.T) {
	f := newFake(t)
	// The test server is https; the plugin turns an http:// link into https, so serve the link as http on the same host.
	httpLink := strings.Replace(f.URL, "https://", "http://", 1)
	f.json("/v1/sub/detail", 200, `{"status":0,"sub":{"subs":[{"id":5,"filename":"x.srt","url":"`+httpLink+`/dl/x","filelist":{}}]}}`)
	f.file("/dl/x", []byte("plain"))
	p := configured(t, f)
	resp, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "5"})
	if err != nil || len(resp.GetFiles()) != 1 || string(resp.GetFiles()[0].GetData()) != "plain" {
		t.Errorf("download with {} file list over an http link = %v, %v", resp, err)
	}
}

func TestDownloadSingleFileAndZip(t *testing.T) {
	f := newFake(t)
	detail(f, `{"status":0,"sub":{"subs":[{"id":1,"filename":"one.SRT","url":"BASE/dl/one"}]}}`)
	f.file("/dl/one", []byte("single"))
	p := configured(t, f)
	resp, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "1"})
	if err != nil || len(resp.GetFiles()) != 1 || resp.GetFiles()[0].GetName() != "one.SRT" || string(resp.GetFiles()[0].GetData()) != "single" {
		t.Fatalf("single = %v, %v", resp, err)
	}

	detail(f, `{"status":0,"sub":{"subs":[{"id":2,"filename":"pack.zip","url":"BASE/dl/zip"}]}}`)
	f.file("/dl/zip", zipOf(t, map[string]string{"dir/Show.S01E01.srt": "e1", "Show.S01E02.ass": "e2", "notes.txt": "n", "Show.sub": "s"}))
	resp, err = p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "2"})
	if err != nil || len(resp.GetFiles()) != 2 {
		t.Fatalf("zip = %v, %v; want the srt and the ass only", resp, err)
	}
	names := map[string]string{}
	for _, x := range resp.GetFiles() {
		names[x.GetName()] = string(x.GetData())
	}
	if names["Show.S01E01.srt"] != "e1" || names["Show.S01E02.ass"] != "e2" {
		t.Errorf("zip entries = %v (paths must be dropped)", names)
	}

	// A format the plugin cannot unpack, or an archive without a single text subtitle, is not found.
	detail(f, `{"status":0,"sub":{"subs":[{"id":3,"filename":"pack.rar","url":"BASE/dl/rar"}]}}`)
	if _, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "3"}); status.Code(err) != codes.NotFound {
		t.Errorf("rar without a file list = %v, want NOT_FOUND", err)
	}
	detail(f, `{"status":0,"sub":{"subs":[{"id":4,"filename":"pack.zip","url":"BASE/dl/zip2"}]}}`)
	f.file("/dl/zip2", zipOf(t, map[string]string{"readme.txt": "n"}))
	if _, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "4"}); status.Code(err) != codes.NotFound {
		t.Errorf("zip without subtitles = %v, want NOT_FOUND", err)
	}
}

func TestDownloadErrors(t *testing.T) {
	f := newFake(t)
	p := configured(t, f)
	for _, id := range []string{"", "abc", "-4", "0"} {
		if _, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: id}); status.Code(err) != codes.InvalidArgument {
			t.Errorf("id %q = %v, want INVALID_ARGUMENT", id, err)
		}
	}
	if len(f.hits) != 0 {
		t.Errorf("bad ids sent %d requests", len(f.hits))
	}
	f.json("/v1/sub/detail", 404, `{"status":20900}`)
	if _, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "9"}); status.Code(err) != codes.NotFound {
		t.Errorf("gone subtitle = %v, want NOT_FOUND", err)
	}
	f.json("/v1/sub/detail", 200, `{"status":0,"sub":{"subs":[]}}`)
	if _, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "9"}); status.Code(err) != codes.NotFound {
		t.Errorf("empty detail = %v, want NOT_FOUND", err)
	}
	f.json("/v1/sub/detail", 403, `{"status":20001}`)
	if _, err := p.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "9"}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("bad token = %v, want FAILED_PRECONDITION", err)
	}
	noToken := newPlugin()
	if _, err := noToken.Download(context.Background(), &pluginv1.DownloadSubtitleRequest{Id: "9"}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("no token = %v", err)
	}
}

func TestFileLanguage(t *testing.T) {
	for name, want := range map[string]string{
		"Movie.chs.srt": "zh-CN", "Movie.CHT.ass": "zh-TW", "电影.简体.srt": "zh-CN", "電影.繁體.srt": "zh-TW", "Movie.big5.srt": "zh-TW",
		"Movie.eng.srt": "en", "Movie.srt": "", "Movie.Chs&Eng.srt": "zh-CN",
	} {
		if got := fileLanguage(name); got != want {
			t.Errorf("fileLanguage(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSubtitleFormat(t *testing.T) {
	for in, want := range map[string]string{"Subrip(srt)": "srt", "ASS/SSA(ass)": "ass", "WebVTT(VTT)": "vtt", "srt": "srt", "": "", "Unknown thing": ""} {
		if got := subtitleFormat(in); got != want {
			t.Errorf("subtitleFormat(%q) = %q, want %q", in, got, want)
		}
	}
}
