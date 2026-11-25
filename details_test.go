package main

import (
	"context"
	"testing"

	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
)

// A recorded-shape search response (made-up names and ids): one subtitle with the optional machine-translation flag, one without any
// optional detail, and ones where the flag and the time have odd shapes.
const detailsBody = `{"status":0,"sub":{"action":"search","result":"succeed","keyword":"x","subs":[
 {"id":701001,"native_name":"测试电影","revision":0,"upload_time":"2015-08-21 22:11:00","subtype":"Subrip(srt)","vote_score":50,"release_site":"made-up group",
  "vote_machine_translate":true,"videoname":"Other.Release.mkv","lang":{"langlist":{"langeng":true},"desc":"英"}},
 {"id":701002,"native_name":"测试电影","subtype":"Subrip(srt)","videoname":"Other.Release.2.mkv","lang":{"langlist":{"langeng":true},"desc":"英"}},
 {"id":701003,"native_name":"测试电影","upload_time":"soon","vote_machine_translate":3,"subtype":"Subrip(srt)","videoname":"Other.Release.3.mkv","lang":{"langlist":{"langeng":true}}},
 {"id":701004,"native_name":"测试电影","upload_time":20150821,"vote_machine_translate":"0","subtype":"Subrip(srt)","videoname":"Other.Release.4.mkv","lang":{"langlist":{"langeng":true}}}
]}}`

func searchDetails(t *testing.T) map[string]*pluginv1.SubtitleCandidate {
	t.Helper()
	f := newFake(t)
	f.json("/v1/sub/search", 200, detailsBody)
	p := configured(t, f)
	resp, err := p.Search(context.Background(), &pluginv1.SearchSubtitlesRequest{Target: movieTarget(), Languages: []string{"en"}})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*pluginv1.SubtitleCandidate{}
	for _, c := range resp.GetCandidates() {
		out[c.GetId()] = c
	}
	if len(out) != 4 {
		t.Fatalf("candidates = %v", out)
	}
	return out
}

func TestCandidateDetailsAreMappedFromTheSearchResult(t *testing.T) {
	c := searchDetails(t)["701001"]
	if c.GetUploadedAt() != "2015-08-21" || !c.GetMachineTranslated() {
		t.Errorf("details = %v", c)
	}
	// Everything else comes only with the detail call, or ASSRT has no such thing; it must stay unknown, never guessed
	// (vote_score has no documented scale, release_site is not a note).
	if c.GetSizeBytes() != 0 || c.GetDurationSeconds() != 0 || c.GetFrameRate() != 0 || c.GetTrusted() || c.GetFileCount() != 0 || c.GetComment() != "" ||
		c.GetDownloadCount() != 0 || c.GetRating() != 0 {
		t.Errorf("unknown fields were filled: %v", c)
	}
}

func TestCandidateDetailsStayUnknownWhenTheSourceGivesNone(t *testing.T) {
	c := searchDetails(t)["701002"]
	if c.GetUploadedAt() != "" || c.GetMachineTranslated() {
		t.Errorf("details = %v", c)
	}
}

func TestOddValuesLeaveDetailsUnknownAndKeepTheSearchWorking(t *testing.T) {
	cs := searchDetails(t)
	if c := cs["701003"]; c.GetUploadedAt() != "" || !c.GetMachineTranslated() {
		t.Errorf("701003 (time \"soon\", flag 3) = %v", c)
	}
	if c := cs["701004"]; c.GetUploadedAt() != "" || c.GetMachineTranslated() {
		t.Errorf("701004 (time a number, flag \"0\") = %v", c)
	}
}

func TestDetailsDoNotChangeConfirmed(t *testing.T) {
	for id, c := range searchDetails(t) {
		if c.GetConfirmed() {
			t.Errorf("candidate %s is confirmed: its video name differs from the file", id)
		}
	}
}

func TestUploadDate(t *testing.T) {
	for in, want := range map[string]string{"2015-08-21 22:11:00": "2015-08-21", "2015-08-21T22:11:00Z": "2015-08-21", "2015-08-21": "2015-08-21", "": "", "soon": "", "2015-13-40 00:00:00": ""} {
		if got := uploadDate(in); got != want {
			t.Errorf("uploadDate(%q) = %q, want %q", in, got, want)
		}
	}
}
