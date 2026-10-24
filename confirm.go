package main

import (
	"path"
	"strings"
	"unicode"
)

// ASSRT has no hash or ID matching: every candidate is a guess by name. The plugin marks one confirmed (SubtitleCandidate.confirmed) only when it was
// found by the video's file name and its video name is that file: the same name, or one name inside the other when the shorter is a real part of
// the longer one (the same release with an extra tag, at least 12 characters and at least two thirds of the longer name). A hit of the title search
// is never confirmed, and neither is one whose video name says nothing. A server that downloads subtitles by itself takes only confirmed candidates
// from this plugin, in the order it returns them.

const (
	// minConfirmedStem is the shortest name that can confirm a candidate by containment; a short stem ("S01E01") is inside many video names.
	minConfirmedStem = 12
	// containmentShare is how much of the longer name the shorter one must be.
	containmentShare = 2.0 / 3.0
)

// fileExts are the extensions that are cut off a name before it is compared. A release name without an extension ends in its group
// ("...x264-GRP"), which path.Ext would take for one.
var fileExts = map[string]bool{"mkv": true, "mp4": true, "avi": true, "ts": true, "m2ts": true, "wmv": true, "mov": true, "rmvb": true, "flv": true, "webm": true,
	"iso": true, "srt": true, "ass": true, "ssa": true, "vtt": true, "sub": true, "idx": true, "zip": true, "rar": true, "7z": true}

// foldName makes a video name comparable: a known extension removed, lower case, runs of characters that are not letters or digits become one space.
func foldName(name string) string {
	if e := strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")); fileExts[e] {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if gap && b.Len() > 0 {
				b.WriteByte(' ')
			}
			gap = false
			b.WriteRune(r)
		} else {
			gap = true
		}
	}
	return b.String()
}

// confirmed reports whether a candidate found by file name is for this video.
func confirmed(fileName, videoName string, byFile bool) bool {
	if !byFile {
		return false
	}
	stem, v := foldName(fileName), foldName(videoName)
	if stem == "" || v == "" {
		return false
	}
	if stem == v {
		return len([]rune(stem)) >= 3
	}
	short, long := stem, v
	if len(v) < len(stem) {
		short, long = v, stem
	}
	return len([]rune(short)) >= minConfirmedStem && strings.Contains(long, short) && float64(len([]rune(short))) >= containmentShare*float64(len([]rune(long)))
}
