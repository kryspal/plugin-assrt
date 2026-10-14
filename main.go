// Command assrt is the official subtitle provider plugin for ASSRT (assrt.net) (change request 0146, capability subtitles.search).
//
// The user enters their own API token from their ASSRT account. The plugin searches subtitles by file name or by title and season/episode,
// and downloads the chosen subtitle (unpacking a zip) for the host to save next to the video; it never keeps download links.
package main

import "github.com/kryspal/plugin-sdk/sdk"

func main() {
	p := newPlugin()
	sdk.Main(sdk.Config{Plugin: p, SubtitleProvider: p})
}
