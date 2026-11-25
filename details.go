package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	pluginv1 "github.com/kryspal/plugin-sdk/pluginv1"
)

// looseString reads a JSON string and leaves anything else (null, a number, an object) empty, so one odd value never fails the whole search.
type looseString string

func (l *looseString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(bytes.TrimSpace(b), &s); err != nil {
		*l = ""
		return nil
	}
	*l = looseString(s)
	return nil
}

// addDetails copies what ASSRT says about a subtitle in a search result into the candidate. It fills only what the source gives: a field
// the source leaves out stays at its default (unknown), and nothing is derived from the file name.
//
// A search result has the upload time and, when users voted so, the machine-translation flag. The download size, the number of downloads and the
// file list only come with the detail call, which costs one of the 20 requests a minute per candidate, so size_bytes, file_count and the rest stay
// unknown. ASSRT has no frame rate, duration, trusted flag or note. vote_score is not mapped: its scale is not documented (the sample response has
// 50), and rating is 0 to 10.
func addDetails(c *pluginv1.SubtitleCandidate, s assrtSub) {
	c.UploadedAt = uploadDate(string(s.UploadTime))
	c.MachineTranslated = truthy(s.VoteMachineTranslate)
}

// uploadDate turns ASSRT's "2015-08-21 22:11:00" into YYYY-MM-DD; a value that is not a date gives "" (unknown).
func uploadDate(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// truthy reads a JSON flag that may be a boolean, a count or a string: true, a number above zero or a string other than "", "0" and "false".
func truthy(raw json.RawMessage) bool {
	var v any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &v); err != nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x > 0
	case string:
		x = strings.ToLower(strings.TrimSpace(x))
		return x != "" && x != "0" && x != "false"
	}
	return false
}
