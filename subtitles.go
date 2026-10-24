package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
	"github.com/kryspal/plugin-sdk/sdk"
)

const (
	defaultLimit = 15 // ASSRT returns at most 15 per request
	// Download limits: the protocol caps the whole response at 5 MB.
	maxFiles     = 6
	maxFileBytes = 2 << 20
	maxTotal     = 5 << 20
)

// textExts are the subtitle formats the plugin returns (the host converts text subtitles; image formats such as VobSub are left out).
var textExts = map[string]bool{"srt": true, "ass": true, "ssa": true, "vtt": true}

// ---- Search ----

type searchResponse struct {
	Sub struct {
		Subs []assrtSub `json:"subs"`
	} `json:"sub"`
}

type assrtSub struct {
	ID         int64  `json:"id"`
	NativeName string `json:"native_name"`
	VideoName  string `json:"videoname"`
	SubType    string `json:"subtype"`
	Lang       struct {
		LangList map[string]bool `json:"langlist"`
		Desc     string          `json:"desc"`
	} `json:"lang"`
}

// Search looks the video up by its file name first and, when that finds nothing, by title (with season and episode). ASSRT has no hash or ID
// matching, so every candidate is matched by name. The host's language filter is applied here: a candidate is kept when one of its languages
// is wanted, and its language is the first wanted one it has.
func (p *plugin) Search(ctx context.Context, req *pluginv1.SearchSubtitlesRequest) (*pluginv1.SearchSubtitlesResponse, error) {
	token, base := p.settings()
	if token == "" {
		return nil, sdk.NeedsConfiguration("ASSRT token is not configured")
	}
	t := req.GetTarget()
	limit := int(req.GetLimit())
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}
	wanted := req.GetLanguages()
	var out []*pluginv1.SubtitleCandidate
	seen := map[int64]bool{}
	for _, q := range searchQueries(t) {
		params := url.Values{"q": {q.text}, "cnt": {strconv.Itoa(defaultLimit)}}
		if q.isFile {
			params.Set("is_file", "1")
			params.Set("no_muxer", "1")
		}
		var resp searchResponse
		err := p.api.call(ctx, token, base+"/sub/search?"+params.Encode(), &resp)
		switch {
		case errors.Is(err, errUnauthorized):
			p.forgetToken(token)
			return nil, sdk.NeedsConfiguration(errUnauthorized.Error())
		case errors.Is(err, errKeywordShort), errors.Is(err, errNoSuchSub):
			continue
		case err != nil:
			return nil, err
		}
		for _, s := range resp.Sub.Subs {
			if s.ID <= 0 || seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			lang, ok := candidateLanguage(s, wanted)
			if !ok {
				continue
			}
			out = append(out, &pluginv1.SubtitleCandidate{Id: strconv.FormatInt(s.ID, 10), Language: lang, ReleaseName: releaseName(s),
				Format: subtitleFormat(s.SubType), MatchedBy: pluginv1.SubtitleCandidate_MATCHED_BY_NAME, Score: nameScore(s, t.GetFileName(), q.isFile),
				Confirmed: confirmed(t.GetFileName(), s.VideoName, q.isFile)})
		}
		if len(out) > 0 {
			break // the file name found subtitles; no need to ask again by title
		}
	}
	// Confirmed candidates first (the server downloads in the order returned), then by score; ties keep ASSRT's order.
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := out[i].GetConfirmed(), out[j].GetConfirmed(); a != b {
			return a
		}
		return out[i].GetScore() > out[j].GetScore()
	})
	if len(out) > limit {
		out = out[:limit]
	}
	confirmedCount := 0
	for _, c := range out {
		if c.GetConfirmed() {
			confirmedCount++
		}
	}
	slog.InfoContext(ctx, "assrt search", slog.Int("candidates", len(out)), slog.Int("confirmed", confirmedCount)) // counts only, no names
	return &pluginv1.SearchSubtitlesResponse{Candidates: out}, nil
}

type searchQuery struct {
	text   string
	isFile bool
}

// searchQueries lists the queries to try in order: the file name without its extension (ASSRT then ignores the release group and video
// parameters), then the title, with season and episode for an episode. The keyword must be at least three characters.
func searchQueries(t *pluginv1.SubtitleTarget) []searchQuery {
	var qs []searchQuery
	if stem := strings.TrimSuffix(t.GetFileName(), path.Ext(t.GetFileName())); len([]rune(stem)) >= 3 {
		qs = append(qs, searchQuery{text: stem, isFile: true})
	}
	if title := strings.TrimSpace(t.GetTitle()); title != "" {
		text := title
		if t.GetType() == pluginv1.ItemType_ITEM_TYPE_EPISODE && t.SeasonNumber != nil && t.EpisodeNumber != nil {
			text = fmt.Sprintf("%s S%02dE%02d", title, t.GetSeasonNumber(), t.GetEpisodeNumber())
		} else if t.GetYear() > 0 {
			text = fmt.Sprintf("%s %d", title, t.GetYear())
		}
		qs = append(qs, searchQuery{text: text})
	}
	return qs
}

// langKeys maps ASSRT's language flags to BCP 47, in the order ASSRT lists them.
var langKeys = []struct{ key, tag string }{
	{"langchs", "zh-CN"}, {"langcht", "zh-TW"}, {"langeng", "en"}, {"langjpn", "ja"}, {"langkor", "ko"},
}

// subLanguages lists the languages of a subtitle. When the flags are missing it falls back to the description (the language names written in Chinese: simplified, traditional, English).
func subLanguages(s assrtSub) []string {
	var out []string
	for _, k := range langKeys {
		if s.Lang.LangList[k.key] {
			out = append(out, k.tag)
		}
	}
	if len(out) == 0 {
		for _, d := range []struct{ word, tag string }{{"简", "zh-CN"}, {"繁", "zh-TW"}, {"英", "en"}, {"日", "ja"}, {"韩", "ko"}} {
			if strings.Contains(s.Lang.Desc, d.word) {
				out = append(out, d.tag)
			}
		}
	}
	return out
}

// sameLanguage reports whether a subtitle language satisfies a wanted one: Chinese is matched by script when the wanted tag says which
// (zh-TW, zh-Hant, zh-HK → traditional; zh-CN, zh-Hans, zh-SG → simplified); a plain "zh" accepts both.
func sameLanguage(have, want string) bool {
	have, want = strings.ToLower(have), strings.ToLower(want)
	if have == want {
		return true
	}
	if !strings.HasPrefix(have, "zh") || !strings.HasPrefix(want, "zh") {
		return strings.SplitN(have, "-", 2)[0] == strings.SplitN(want, "-", 2)[0] && !strings.Contains(want, "-")
	}
	switch want {
	case "zh":
		return true
	case "zh-tw", "zh-hant", "zh-hk", "zh-mo":
		return have == "zh-tw"
	case "zh-cn", "zh-hans", "zh-sg":
		return have == "zh-cn"
	}
	return false
}

// candidateLanguage picks the language of a candidate: the first wanted language it has; with no wanted languages, its first language.
// It reports false when languages are wanted and it has none of them (or none known).
func candidateLanguage(s assrtSub, wanted []string) (string, bool) {
	have := subLanguages(s)
	if len(wanted) == 0 {
		if len(have) == 0 {
			return "", true
		}
		return have[0], true
	}
	for _, w := range wanted {
		for _, h := range have {
			if sameLanguage(h, w) {
				return h, true
			}
		}
	}
	return "", false
}

func releaseName(s assrtSub) string {
	if s.VideoName != "" {
		return s.VideoName
	}
	return s.NativeName
}

var formatRE = regexp.MustCompile(`\(([A-Za-z0-9]+)\)`)

// subtitleFormat reads the extension out of ASSRT's subtype ("Subrip(srt)", "ASS/SSA(ass)"); an unrecognized subtype gives "".
func subtitleFormat(subtype string) string {
	if m := formatRE.FindStringSubmatch(subtype); m != nil {
		return strings.ToLower(m[1])
	}
	if f := strings.ToLower(strings.TrimSpace(subtype)); textExts[f] || f == "zip" {
		return f
	}
	return ""
}

// nameScore rates how close the subtitle's video name is to the file: 1 for the same name, 0.8 when one contains the other, else 0.5 for a
// file-name search hit (ASSRT already matched it) and 0.3 for a title search hit.
func nameScore(s assrtSub, fileName string, byFile bool) float64 {
	stem := strings.ToLower(strings.TrimSuffix(fileName, path.Ext(fileName)))
	v := strings.ToLower(strings.TrimSuffix(s.VideoName, path.Ext(s.VideoName)))
	switch {
	case stem != "" && v == stem:
		return 1
	case stem != "" && v != "" && (strings.Contains(v, stem) || strings.Contains(stem, v)):
		return 0.8
	case byFile:
		return 0.5
	}
	return 0.3
}

// ---- Download ----

type detailResponse struct {
	Sub struct {
		Subs []struct {
			Filename string `json:"filename"`
			URL      string `json:"url"`
			// ASSRT sends the file list as an array, or as an empty object {} when there is none.
			FileList json.RawMessage `json:"filelist"`
		} `json:"subs"`
	} `json:"sub"`
}

// Download asks ASSRT for the subtitle's detail (the download URLs are made on request and expire, so nothing is kept), then fetches the
// subtitle files: the entries of the archive's file list when there is one (only text formats), otherwise the file itself, unpacking a zip.
// At most 6 files, 2 MB each and 5 MB in all. ASSRT has no download quota, so the remaining quota is unknown (-1).
func (p *plugin) Download(ctx context.Context, req *pluginv1.DownloadSubtitleRequest) (*pluginv1.DownloadSubtitleResponse, error) {
	token, base := p.settings()
	if token == "" {
		return nil, sdk.NeedsConfiguration("ASSRT token is not configured")
	}
	id, err := strconv.ParseInt(req.GetId(), 10, 64)
	if err != nil || id <= 0 {
		return nil, sdk.InvalidArgument("assrt: bad subtitle id")
	}
	var detail detailResponse
	err = p.api.call(ctx, token, base+"/sub/detail?id="+strconv.FormatInt(id, 10), &detail)
	switch {
	case errors.Is(err, errUnauthorized):
		p.forgetToken(token)
		return nil, sdk.NeedsConfiguration(errUnauthorized.Error())
	case errors.Is(err, errNoSuchSub):
		return nil, sdk.NotFound("assrt: the subtitle no longer exists")
	case errors.Is(err, errKeywordShort):
		return nil, sdk.InvalidArgument("assrt: bad subtitle id")
	case err != nil:
		return nil, err
	}
	if len(detail.Sub.Subs) == 0 {
		return nil, sdk.NotFound("assrt: the subtitle no longer exists")
	}
	d := detail.Sub.Subs[0]
	var fileList []struct {
		F   string `json:"f"`
		URL string `json:"url"`
	}
	if bytes.HasPrefix(bytes.TrimSpace(d.FileList), []byte("[")) {
		_ = json.Unmarshal(d.FileList, &fileList)
	}
	resp := &pluginv1.DownloadSubtitleResponse{QuotaRemaining: -1}
	total := 0
	add := func(name string, data []byte) {
		if len(resp.Files) >= maxFiles || len(data) == 0 || total+len(data) > maxTotal {
			return
		}
		total += len(data)
		resp.Files = append(resp.Files, &pluginv1.SubtitleFile{Name: name, Language: fileLanguage(name), Data: data})
	}
	if len(fileList) > 0 {
		for _, f := range fileList {
			name := path.Base(strings.ReplaceAll(f.F, "\\", "/"))
			if !textExts[ext(name)] || len(resp.Files) >= maxFiles {
				continue
			}
			data, err := p.api.fetch(ctx, f.URL, maxFileBytes)
			if err != nil {
				return nil, err
			}
			add(name, data)
		}
	} else if name := path.Base(strings.ReplaceAll(d.Filename, "\\", "/")); textExts[ext(name)] || ext(name) == "zip" {
		data, err := p.api.fetch(ctx, d.URL, maxTotal)
		if err != nil {
			return nil, err
		}
		if ext(name) == "zip" {
			for _, f := range unzip(data) {
				add(f.name, f.data)
			}
		} else {
			add(name, data)
		}
	}
	if len(resp.Files) == 0 {
		return nil, sdk.NotFound("assrt: no usable subtitle file (only srt, ass, ssa and vtt, loose or in a zip, are supported)")
	}
	return resp, nil
}

func ext(name string) string { return strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")) }

type unzipped struct {
	name string
	data []byte
}

// unzip returns the text subtitle files of a zip (at most maxFiles, each up to maxFileBytes); a broken archive gives none.
func unzip(data []byte) []unzipped {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	var out []unzipped
	for _, f := range zr.File {
		name := path.Base(strings.ReplaceAll(f.Name, "\\", "/"))
		if f.FileInfo().IsDir() || !textExts[ext(name)] || f.UncompressedSize64 > maxFileBytes || len(out) >= maxFiles {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxFileBytes+1))
		_ = rc.Close()
		if err != nil || len(b) > maxFileBytes {
			continue
		}
		out = append(out, unzipped{name: name, data: b})
	}
	return out
}

// fileLanguage guesses the language from a subtitle file name (".chs", ".cht", ".eng", or the Chinese words for simplified and traditional);
// it returns "" when the name says nothing, and the host then uses the language it searched for.
func fileLanguage(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "繁") || strings.Contains(n, ".cht") || strings.Contains(n, ".zh-tw") || strings.Contains(n, "big5"):
		return "zh-TW"
	case strings.Contains(n, "简") || strings.Contains(n, "簡") || strings.Contains(n, ".chs") || strings.Contains(n, ".zh-cn"):
		return "zh-CN"
	case strings.Contains(n, ".eng") || strings.Contains(n, ".en."):
		return "en"
	}
	return ""
}
