package main

import "github.com/kryspal/plugin-sdk/i18n"

// messages is the declared string table (change request 0057): en, zh-TW and zh-CN; other languages follow the i18n fallback chain, ending in English.
// The attribution (attribution.text) is the original text, the same in every language, and is not here.
var messages = i18n.New(messageTables)

var messageTables = map[string]map[string]string{
	"en": {
		"desc":           "Finds and downloads subtitles from ASSRT (assrt.net), a large community library of Chinese and bilingual subtitles for movies and series. It searches by the video's file name and then by title, season and episode, and saves the subtitle you choose next to the video. Choose this plugin when searching for subtitles; it is free for personal use with your own token.",
		"groupAdvanced":  "Advanced",
		"tokenLabel":     "ASSRT API token",
		"tokenHelp":      "Your own API token from your ASSRT account page (32 letters and digits). It is sent only to ASSRT.",
		"tokenLink":      "Open the ASSRT account page",
		"baseURLLabel":   "API base URL",
		"baseURLHelp":    "Leave empty to use the official API. Only for a trusted HTTPS proxy.",
		"errRequired":    "Enter your ASSRT API token.",
		"errTokenFormat": "An ASSRT token is 32 letters and digits.",
		"errHTTPS":       "Must be an https:// address.",
	},
	"zh-TW": {
		"desc":           "從 ASSRT（assrt.net）搜尋並下載字幕。它是電影與劇集的大型社群字幕庫，中文與雙語字幕特別多。先依影片檔名搜尋，找不到再依片名、季與集搜尋，並把你選的字幕存到影片旁。搜尋字幕時可選用這個外掛；個人使用免費，需要你自己的 token。",
		"groupAdvanced":  "進階",
		"tokenLabel":     "ASSRT API token",
		"tokenHelp":      "你自己的 ASSRT 帳號頁面上的 API token（32 個英數字），只會送給 ASSRT。",
		"tokenLink":      "開啟 ASSRT 帳號頁面",
		"baseURLLabel":   "API 網址",
		"baseURLHelp":    "留空表示使用官方 API。只用於可信任的 HTTPS 代理。",
		"errRequired":    "請填入你的 ASSRT API token。",
		"errTokenFormat": "ASSRT 的 token 是 32 個英數字。",
		"errHTTPS":       "必須是 https:// 網址。",
	},
	"zh-CN": {
		"desc":           "从 ASSRT（assrt.net）搜索并下载字幕。它是电影和剧集的大型社区字幕库，中文和双语字幕特别多。先按视频文件名搜索，找不到再按片名、季和集搜索，并把你选的字幕保存到视频旁。搜索字幕时可选用这个插件；个人使用免费，需要你自己的 token。",
		"groupAdvanced":  "高级",
		"tokenLabel":     "ASSRT API token",
		"tokenHelp":      "你自己的 ASSRT 账号页面上的 API token（32 个字母和数字），只会发送给 ASSRT。",
		"tokenLink":      "打开 ASSRT 账号页面",
		"baseURLLabel":   "API 网址",
		"baseURLHelp":    "留空表示使用官方 API。只用于可信任的 HTTPS 代理。",
		"errRequired":    "请填入你的 ASSRT API token。",
		"errTokenFormat": "ASSRT 的 token 是 32 个字母和数字。",
		"errHTTPS":       "必须是 https:// 网址。",
	},
}
