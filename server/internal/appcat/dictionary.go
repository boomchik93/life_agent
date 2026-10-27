package appcat

import "strings"

// dictionary holds well-known Android packages, Mac apps and domains. It is
// the first layer after the cache; the model sees only what is not here.
var dictionary = map[string]string{
	// career
	"com.github.android": "career", "github.com": "career", "gitlab.com": "career", "stackoverflow.com": "career",
	"leetcode.com": "career", "codeforces.com": "career", "hackerrank.com": "career", "habr.com": "career",
	"hh.ru": "career", "linkedin.com": "career", "com.linkedin.android": "career", "ru.hh.android": "career",
	"visual studio code": "career", "xcode": "career", "intellij idea": "career", "pycharm": "career",
	"goland": "career", "android studio": "career", "terminal": "career", "iterm2": "career", "iterm": "career",
	"com.termux": "career", "dev.leetcode": "career",
	// study
	"coursera.org": "study", "stepik.org": "study", "edx.org": "study", "khanacademy.org": "study",
	"wikipedia.org": "study", "scholar.google.com": "study", "arxiv.org": "study", "duolingo.com": "study",
	"com.duolingo": "study", "org.coursera.android": "study", "org.stepik.android": "study", "ru.stepik.android": "study",
	"com.anki": "study", "com.ichi2.anki": "study", "anki": "study", "notion.so": "study",
	"com.google.android.apps.classroom": "study", "classroom.google.com": "study", "ru.sdo": "study",
	// work
	"slack": "work", "com.slack": "work", "zoom.us": "work", "us.zoom.videomeetings": "work", "microsoft teams": "work",
	"com.microsoft.teams": "work", "com.microsoft.office.outlook": "work", "outlook": "work", "mail.google.com": "work",
	"com.google.android.gm": "work", "docs.google.com": "work", "sheets.google.com": "work", "jira": "work",
	"atlassian.net": "work", "figma.com": "work", "com.figma.mirror": "work", "microsoft word": "work",
	"microsoft excel": "work", "microsoft powerpoint": "work", "pages": "work", "numbers": "work", "keynote": "work",
	"com.google.android.apps.docs.editors.docs": "work", "com.google.android.apps.docs.editors.sheets": "work",
	// sport
	"com.strava": "sport", "strava.com": "sport", "com.nike.ntc": "sport", "com.nike.plusgps": "sport",
	"com.runtastic.android": "sport", "com.google.android.apps.fitness": "sport", "ru.sportmaster": "sport",
	"com.fitnesskeeper.runkeeper": "sport", "com.garmin.android.apps.connectmobile": "sport",
	// social
	"org.telegram.messenger": "social", "org.telegram.messenger.web": "social", "web.telegram.org": "social",
	"telegram": "social", "com.whatsapp": "social", "whatsapp": "social", "com.vkontakte.android": "social",
	"vk.com": "social", "com.vk.im": "social", "com.instagram.android": "social", "instagram.com": "social",
	"com.discord": "social", "discord": "social", "discord.com": "social", "com.facebook.katana": "social",
	"facebook.com": "social", "com.twitter.android": "social", "x.com": "social", "twitter.com": "social",
	"com.snapchat.android": "social", "com.viber.voip": "social", "messages": "social",
	"com.samsung.android.messaging": "social", "com.google.android.apps.messaging": "social",
	"com.reddit.frontpage": "social", "reddit.com": "social", "ok.ru": "social", "ru.ok.android": "social",
	// leisure
	"com.google.android.youtube": "leisure", "youtube.com": "leisure", "com.netflix.mediaclient": "leisure",
	"netflix.com": "leisure", "com.spotify.music": "leisure", "spotify": "leisure", "open.spotify.com": "leisure",
	"com.zhiliaoapp.musically": "leisure", "tiktok.com": "leisure", "twitch.tv": "leisure",
	"tv.twitch.android.app": "leisure", "steam": "leisure", "steampowered.com": "leisure", "store.steampowered.com": "leisure",
	"com.valvesoftware.android.steam.community": "leisure", "kinopoisk.ru": "leisure", "ru.kinopoisk": "leisure",
	"ru.yandex.music": "leisure", "music.yandex.ru": "leisure", "com.google.android.apps.youtube.music": "leisure",
	"epic games launcher": "leisure", "battle.net": "leisure", "dota 2": "leisure", "counter-strike 2": "leisure",
	"pikabu.ru": "leisure", "9gag.com": "leisure", "com.mojang.minecraftpe": "leisure",
}

// Lookup finds a name in the dictionary: exactly, or as a sub-domain of a
// listed domain ("gist.github.com" is "github.com").
func Lookup(key string) (string, bool) {
	if c, ok := dictionary[key]; ok {
		return c, true
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			if c, ok := dictionary[key[i+1:]]; ok && strings.Contains(key[i+1:], ".") {
				return c, true
			}
		}
	}
	return "", false
}
