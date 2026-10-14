# plugin-assrt

Search and download subtitles from ASSRT (assrt.net), a large library of Chinese and bilingual subtitles. You enter your own token. An official [Kryspal Media](https://github.com/kryspal) plugin, built on [plugin-sdk](https://github.com/kryspal/plugin-sdk).

```sh
go build -o assrt .
go vet ./... && go test ./...
```

Implementation notes: [docs/design.md](docs/design.md).

Licensed under the Apache License 2.0 (see `LICENSE` and `NOTICE`).

Official builds are signed and published through the Kryspal plugin catalog; builds from source are treated as third-party plugins.
