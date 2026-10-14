# assrt: ASSRT subtitle provider plugin

Owner: A4. Official plugin (closed source, change request 0117), implementing the protocol v1 `SubtitleProvider` service with `github.com/kryspal/plugin-sdk/sdk` (change request 0146, phase 1). It declares the capability `subtitles.search` and no feature code: the host's capability layer (and `SubtitleService`) decides whether subtitle search is licensed, so the plugin itself is never locked and can always be installed, configured and enabled. Paid features are now gated by the host's capability layer instead of the plugin (1.0.2). It is bundled in the image and reserved as an official ID; after the administrator enters their token and enables it on the plugin page, the subtitle search of the host (`internal/subtitle`) uses it together with the other subtitle providers. The plugin only returns bytes; saving the file next to the video is the host's job.

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

## Settings

| Key | Type | Description |
| --- | --- | --- |
| `token` | secret, required | The ASSRT API token (32 letters and digits); `Configure` checks the shape, not the network |
| `base_url` | string (advanced) | Defaults to `https://api.assrt.net/v1`; use only with a trusted HTTPS proxy |

## Version and bundling

The version number is written only in `VERSION` (`go:embed`, also the `<version>` of the bundled directory). `deploy/plugins/build.sh` builds it; `CATALOG.json` is the display data for the plugin catalog.

## Tests

`go test ./plugins/assrt` (`-short` skips the process test): `subtitles_test.go` runs against a fake HTTPS ASSRT with recorded-shape responses and made-up names (file-name query, title fallback, episode query and limit, nothing to ask, language matching, every status, quota window, spacing, file list, loose file, zip, rar without a list, the `{}` file list and `http://` links, errors); `plugin_test.go` has `TestDescribe`, `TestConfigure` and `TestPluginProcess`. Tests never call the real API.
