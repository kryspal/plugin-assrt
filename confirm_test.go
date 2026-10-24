package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
)

func TestConfirmed(t *testing.T) {
	const file = "Made.Up.Movie.2019.1080p.BluRay.x264-GRP.mkv"
	for _, tc := range []struct {
		name        string
		file, video string
		byFile      bool
		want        bool
	}{
		{"same name", file, "Made.Up.Movie.2019.1080p.BluRay.x264-GRP.mkv", true, true},
		{"same name, other case and separators", file, "made up movie 2019 1080p bluray x264-grp.mp4", true, true},
		{"same release with an extra tag", file, "Made.Up.Movie.2019.1080p.BluRay.x264-GRP.CHS", true, true},
		{"the video name is a part of the file name", "Made.Up.Movie.2019.1080p.BluRay.x264-GRP.REPACK.mkv", "Made.Up.Movie.2019.1080p.BluRay.x264-GRP", true, true},
		{"another release of the same film", file, "Made.Up.Movie.2019.720p.WEB-DL.x264-OTHER.mkv", true, false},
		{"only the title", file, "Made.Up.Movie.2019", true, false},
		{"a short stem inside a longer name", "S01E01.mkv", "Show.Name.S01E01.1080p.WEB.x264-GRP.mkv", true, false},
		{"found by title, same name", file, file, false, false},
		{"no video name", file, "", true, false},
		{"no file name", "", file, true, false},
	} {
		if got := confirmed(tc.file, tc.video, tc.byFile); got != tc.want {
			t.Errorf("%s: confirmed = %v, want %v", tc.name, got, tc.want)
		}
	}
}

const confirmBody = `{"status":0,"sub":{"subs":[
 {"id":701,"native_name":"a","videoname":"Made.Up.Movie.2019.720p.WEB-DL.x264-OTHER.mkv","subtype":"Subrip(srt)","lang":{"langlist":{"langchs":true}}},
 {"id":702,"native_name":"b","videoname":"Made.Up.Movie.2019.mkv","subtype":"Subrip(srt)","lang":{"langlist":{"langchs":true}}},
 {"id":703,"native_name":"c","videoname":"Made.Up.Movie.2019.1080p.BluRay.x264-GRP.mkv","subtype":"Subrip(srt)","lang":{"langlist":{"langchs":true}}}
]}}`

// Found by the file name, a subtitle made for this very file is confirmed and comes first; the others stay candidates.
func TestFileNameHitsAreConfirmedOnlyForThisFile(t *testing.T) {
	f := newFake(t)
	f.json("/v1/sub/search", 200, confirmBody)
	p := configured(t, f)
	resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: movieTarget(), Languages: []string{"zh-CN"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range resp.GetCandidates() {
		got = append(got, c.GetId()+map[bool]string{true: "+", false: "-"}[c.GetConfirmed()])
	}
	if want := "703+,702-,701-"; strings.Join(got, ",") != want {
		t.Errorf("candidates = %s, want %s", strings.Join(got, ","), want)
	}
}

// A title search hit is a guess about the work, whatever its video name looks like: never confirmed.
func TestTitleSearchHitsAreNeverConfirmed(t *testing.T) {
	f := newFake(t)
	f.routes["/v1/sub/search"] = func(w http.ResponseWriter, r *http.Request) {
		body := `{"status":0,"sub":{"subs":[]}}`
		if r.URL.Query().Get("is_file") == "" {
			body = confirmBody
		}
		_, _ = w.Write([]byte(body))
	}
	p := configured(t, f)
	resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: movieTarget(), Languages: []string{"zh-CN"}})
	if err != nil || len(resp.GetCandidates()) == 0 {
		t.Fatalf("Search = %v, %v", resp, err)
	}
	for _, c := range resp.GetCandidates() {
		if c.GetConfirmed() {
			t.Errorf("title search hit confirmed: %v", c)
		}
	}
}
