# assrt: ASSRT subtitle provider plugin

An official Kryspal Media plugin that searches and downloads subtitles from ASSRT. It implements the protocol v1 `SubtitleProvider` service with [`github.com/kryspal/plugin-sdk`](https://github.com/kryspal/plugin-sdk) and declares the capability `subtitles.search`. The plugin only returns subtitle bytes; saving the file next to the video is the server's job. The plugin itself is never locked: whether subtitle search is available is decided by the server.

## Data source and terms

ASSRT (assrt.net) is free for personal use. The terms ask to credit the site and never to keep download links (each is unique and expires). The plugin:

- uses **the user's own token** (32 letters and digits, from their ASSRT account page), sent only to ASSRT in the `Authorization: Bearer` header, never in a URL or log;
- **keeps nothing**: no download link or file is stored; the detail request is made at download time;
- stays under the quota of **20 requests per minute per token and IP**: one request at a time, at least 400 ms apart, and at most 18 API requests in any minute (past that it returns `RESOURCE_EXHAUSTED` with the time to wait, without sending anything, so the host pauses the plugin); over-quota (30900) and HTTP 429 also return `RESOURCE_EXHAUSTED`;
- **attribution**: "Subtitles provided by assrt.net" goes in the manifest's attribution (the source line on the plugin page, with a link).

## API

- Search: `GET {base}/sub/search?q=…&cnt=15`, adding `is_file=1&no_muxer=1` for the file-name query. The first query is the video's file name without its extension (skipped when shorter than three characters); only when it finds nothing is a second query sent: the title with the year (movies) or `S01E02` (episodes). Candidates are deduplicated by id and **all matched by name** (ASSRT has no hash or ID matching).
- Language: ASSRT's flags `langchs`, `langcht`, `langeng`, `langjpn`, `langkor` (or the description, which lists the language names as the Chinese characters for simplified, traditional and English, when the flags are missing) become `zh-CN`, `zh-TW`, `en`, `ja`, `ko`. Chinese is matched by script: `zh-TW`, `zh-Hant`, `zh-HK` accept traditional, `zh-CN`, `zh-Hans`, `zh-SG` simplified, a plain `zh` both. When languages are wanted, a candidate with none of them is dropped, and its language is the first wanted one it has; with none wanted it gets its first language.
- Format: the extension in the `subtype` ("Subrip(srt)", "ASS/SSA(ass)"; a bare "SSA"/"srt" also works). Score: 1 when the subtitle's video name equals the file name, 0.8 when one contains the other, 0.5 for a file-name hit, 0.3 for a title hit.
- Download: `GET {base}/sub/detail?id=…`, then the files over **https** (ASSRT sends `http://` links; the same host serves https; its 302 redirects are followed). With a file list (`filelist`, an array, or an empty object `{}` when there is none) the text entries are fetched one by one; otherwise the file itself, unpacking a zip. Only `srt`, `ass`, `ssa`, `vtt`; at most 6 files, 2 MB each, 5 MB in all; paths are dropped. Language is guessed from the file name (`.chs`, `.cht`, the Chinese characters for simplified and traditional, `.eng`) or left empty (the host then uses the searched language). The remaining quota is unknown (-1): ASSRT has no download quota.
- Errors: statuses 1 and 20001 (bad token) make the plugin forget the token and report needs-configuration; 20900 is not found; 101 (keyword too short) gives no results; 30000–30300 and connection errors are unavailable.

Not supported: rar archives without a file list (the plugin does not unpack rar), VobSub and other image formats, and `.txt` readmes.

## Confirmed (1.1.0)

`confirmed(fileName, videoName, byFile)` in `confirm.go`. Only candidates found by the file name search (`is_file=1`) can be confirmed, and only when the folded video name (`foldName`: a known video or subtitle extension cut off, lower case, runs of non-letters and non-digits as one space) equals the folded file name, or one contains the other with the shorter at least 12 characters and at least two thirds of the longer (`minConfirmedStem`, `containmentShare`). Title search hits are never confirmed. Results are sorted confirmed first, then by score (stable, so ASSRT's own order breaks ties). The extension list exists because `path.Ext` takes the release group in "...x264-GRP" for an extension.

## Settings

| Key | Type | Description |
| --- | --- | --- |
| `token` | secret, required | The ASSRT API token (32 letters and digits); `Configure` checks the shape, not the network |
| `base_url` | string (advanced) | Defaults to `https://api.assrt.net/v1`; use only with a trusted HTTPS proxy |

## Version and bundling

The version number is written only in `VERSION` (embedded with `go:embed`; it is also the plugin's reported version). `CATALOG.json` holds the display data for the plugin catalog. Releases are built by the CI workflows in `.github/workflows/`: a `v*` tag builds linux/amd64 and linux/arm64 packages and creates a GitHub Release. Any change that alters the binary needs a new `VERSION`; a published version is never changed.

## Tests

`go test ./...` (`-short` skips the process test): `subtitles_test.go` runs against a fake HTTPS ASSRT with recorded-shape responses and made-up names (file-name query, title fallback, episode query and limit, nothing to ask, language matching, every status, quota window, spacing, file list, loose file, zip, rar without a list, the `{}` file list and `http://` links, errors); `plugin_test.go` has `TestDescribe`, `TestConfigure` and `TestPluginProcess`. Tests never call the real API.

## Official builds and releases

This repository is the source code of the official Kryspal Media `assrt` plugin. Official versions are built by this repository's GitHub Actions workflow from a `v*` tag, attached to a GitHub Release, and then added to the Kryspal Media plugin catalog, which Kryspal signs. The Kryspal Media server installs and updates official plugins from that signed catalog and verifies the signature.

A binary you build from this source yourself is **not** signed by Kryspal and is not part of the official catalog. You are free to build, modify and run it under the Apache License 2.0, but the server treats it as a third-party plugin.

Release process (maintainers): update `VERSION` and `CHANGELOG.md` for any change that alters the binary, wait for the CI workflow to pass on `main`, then push an annotated tag `vX.Y.Z` that matches `VERSION`. The release workflow builds linux/amd64 and linux/arm64 packages, creates the GitHub Release and notifies the main repository, which verifies the packages and publishes the catalog. A published version is never changed.

## License

Apache License 2.0, see `LICENSE` and `NOTICE`.
