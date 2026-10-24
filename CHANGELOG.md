# Changelog

All notable changes to this plugin are listed here. The version in `VERSION` is the single source of the version number; a published version is never changed.

## 1.1.0

- A subtitle found by the video's file name is marked confirmed (`SubtitleCandidate.confirmed`) when its video name is that file (the same name, or one inside the other when the shorter is a real part of the longer: at least 12 characters and two thirds of it). A title search hit is never confirmed. Candidates are ordered confirmed first, then by score. A server that downloads subtitles by itself takes only confirmed candidates from this plugin; a server that does not know the field ignores it.
- Built with plugin SDK v0.8.0.

## 1.0.6

- Built with plugin SDK v0.7.0. No functional change.

## 1.0.5

- Built with plugin SDK v0.5.0. No functional change.

## 1.0.4

- Built with plugin SDK v0.2.0. No functional change.
- Needs server 0.9.2 or newer.
- Includes the documentation, release workflow and source cleanup done since the last published version.

## 1.0.3

- Rebuilt with Go 1.27.2 and golang.org/x/net v0.60.0 (security fixes).
- Includes the changes since the last published catalog version (1.0.1): the subtitle plugin is never locked (availability is decided by the server), plus the documentation and source cleanup for the public repository.

## 1.0.2

- Subtitle search and download on ASSRT; the plugin is never locked, availability is decided by the server.
- Development history before this release lives in the Kryspal Media main repository; this repository starts from this version.
