# plugin-assrt

Search and download subtitles from ASSRT (assrt.net), a large library of Chinese and bilingual subtitles. You enter your own token. An official [Kryspal Media](https://github.com/kryspal) plugin, built on [plugin-sdk](https://github.com/kryspal/plugin-sdk).

```sh
go build -o assrt .
go vet ./... && go test ./...
```

## Automatic download

ASSRT has no hash or ID matching, so every candidate is a guess by name. A server that downloads subtitles by itself takes only the candidates the plugin marks as confirmed (`SubtitleCandidate.confirmed`) from this plugin, and the plugin marks one only when it was found by the video's file name and its video name is that file: the same name (case, separators and a known extension ignored), or one name inside the other when the shorter is at least 12 characters and at least two thirds of the longer (the same release with an extra tag). A hit of the title search is never confirmed. Confirmed candidates come first, then the rest by score; everything not confirmed stays in the list for the user to choose from. A server that does not know the field ignores it.

Implementation notes: [docs/design.md](docs/design.md).

Licensed under the Apache License 2.0 (see `LICENSE` and `NOTICE`).

Official builds are signed and published through the Kryspal plugin catalog; builds from source are treated as third-party plugins.
