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
	"com.runtastic.android": "sport", "com.google.android.apps.fitness": "sport", "com.fitnesskeeper.runkeeper": "sport", "com.garmin.android.apps.connectmobile": "sport",
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
	// career, more
	"linkedin": "career", "code": "career", "cursor": "career", "zed": "career", "warp": "career", "ghostty": "career",
	"docker desktop": "career", "docker": "career", "postman": "career", "tableplus": "career",
	"dbeaver": "career", "sublime text": "career", "webstorm": "career", "clion": "career",
	"datagrip": "career", "fork": "career", "github desktop": "career", "sourcetree": "career",
	"simulator": "career", "go.dev": "career", "pkg.go.dev": "career", "pub.dev": "career",
	"docs.flutter.dev": "career", "flutter.dev": "career", "developer.android.com": "career",
	"developer.apple.com": "career", "developer.mozilla.org": "career", "docs.python.org": "career",
	"python.org": "career", "npmjs.com": "career", "vercel.com": "career", "openrouter.ai": "career",
	"kaggle.com": "career", "acm.timus.ru": "career", "informatics.msk.ru": "career", "e-olymp.com": "career",
	"atcoder.jp": "career", "neetcode.io": "career", "roadmap.sh": "career", "career.habr.com": "career",
	"getmatch.ru": "career", "superjob.ru": "career", "com.superjob.android": "career",
	"io.github.mobile": "career", "com.jetbrains.space": "career",
	// study, more
	"obsidian": "study", "notion": "study", "quizlet.com": "study", "com.quizlet.quizletandroid": "study", "memrise.com": "study",
	"com.memrise.android.memrisecompanion": "study", "lingualeo.com": "study", "com.lingualeo.android": "study",
	"skyeng.ru": "study", "ru.skyeng.vimbox": "study", "puzzle-english.com": "study", "openedu.ru": "study",
	"ulearn.me": "study", "urfu.ru": "study", "elearn.urfu.ru": "study", "moodle": "study",
	"foxford.ru": "study", "ru.foxford.app": "study", "uchi.ru": "study", "practicum.yandex.ru": "study",
	"skillbox.ru": "study", "netology.ru": "study", "geekbrains.ru": "study", "stepik": "study",
	"wolframalpha.com": "study", "desmos.com": "study", "mathway.com": "study", "photomath": "study",
	"com.microblink.photomath": "study", "translate.google.com": "study",
	"com.google.android.apps.translate": "study", "deepl.com": "study", "reverso.net": "study",
	"context.reverso.net": "study", "gramota.ru": "study", "dnevnik.ru": "study", "ru.dnevnik.app": "study",
	"chegg.com": "study", "studfile.net": "study", "studopedia.ru": "study", "cyberleninka.ru": "study",
	"elibrary.ru": "study", "scholar.google.ru": "study", "researchgate.net": "study", "overleaf.com": "study",
	"sci-hub.ru": "study", "libgen.is": "study", "z-library.sk": "study",
	// work, more
	"claude": "work", "chatgpt": "work", "com.anthropic.claude": "work", "com.openai.chatgpt": "work",
	"claude.ai": "work", "chatgpt.com": "work", "gemini.google.com": "work",
	"com.google.android.apps.bard": "work", "perplexity.ai": "work", "ai.perplexity.app.android": "work",
	"deepseek.com": "work", "chat.deepseek.com": "work", "com.deepseek.chat": "work", "gigachat.ru": "work",
	"alice.yandex.ru": "work", "mail": "work", "e.mail.ru": "work",
	"ru.mail.mailapp": "work", "mail.yandex.ru": "work", "ru.yandex.mail": "work", "spark": "work",
	"thunderbird": "work", "calendar": "work", "todoist": "work", "com.todoist": "work",
	"todoist.com": "work", "trello.com": "work", "com.trello": "work", "miro.com": "work",
	"com.realtimeboard": "work", "linear.app": "work", "asana.com": "work", "clickup.com": "work",
	"yougile.com": "work", "teams.microsoft.com": "work", "meet.google.com": "work",
	"telemost.yandex.ru": "work", "ru.yandex.telemost": "work", "com.google.android.apps.meetings": "work",
	"webex": "work", "skype": "work", "com.skype.raider": "work", "disk.yandex.ru": "work",
	"ru.yandex.disk": "work", "dropbox": "work", "onedrive": "work", "microsoft onenote": "work", "com.microsoft.office.word": "work",
	"com.microsoft.office.excel": "work", "com.microsoft.office.officehubrow": "work",
	// sport, more
	"com.adidas.app": "sport", "com.adidas.running": "sport", "ru.worldclass": "sport", "ru.xfit": "sport",
	"com.sportsbox.fitness": "sport", "com.fiton.android": "sport", "com.jefit.app": "sport",
	"com.hevy": "sport", "com.strong.app": "sport", "com.fitifyworkouts.bodyweight.workoutapp": "sport",
	"com.sweat.app": "sport", "com.downdogapp": "sport", "com.komoot.android": "sport",
	// social, more
	"telegram desktop": "social", "facetime": "social", "imessage": "social", "signal": "social",
	"org.thoughtcrime.securesms": "social", "com.tinder": "social", "tinder.com": "social",
	"com.badoo.mobile": "social", "ru.mamba.client": "social", "threads.net": "social",
	"com.instagram.barcelona": "social", "pinterest.com": "social", "com.pinterest": "social",
	"ru.max.messenger": "social", "ru.oneme.app": "social", "max.ru": "social",
	"web.max.ru": "social", "com.icq.mobile.client": "social", "tamtam": "social", "web.whatsapp.com": "social",
	"vk.ru": "social", "m.vk.com": "social", "com.vk.vkclient": "social", "skype.com": "work",
	// leisure, more
	"sports.ru": "leisure", "ru.sports.android": "leisure", "championat.com": "leisure",
	"music": "leisure", "tv": "leisure", "podcasts": "leisure", "books": "leisure", "iina": "leisure",
	"vlc": "leisure", "quicktime player": "leisure", "infuse": "leisure", "plex": "leisure",
	"steamwebhelper": "leisure", "minecraft": "leisure", "roblox": "leisure", "com.roblox.client": "leisure",
	"genshin impact": "leisure", "com.mihoyo.genshinimpact": "leisure", "com.supercell.clashofclans": "leisure",
	"com.supercell.brawlstars": "leisure", "com.supercell.clashroyale": "leisure",
	"com.king.candycrushsaga": "leisure", "com.pubg.imobile": "leisure", "com.tencent.ig": "leisure",
	"com.activision.callofduty.shooter": "leisure", "com.innersloth.spacemafia": "leisure",
	"com.chess": "leisure", "chess.com": "leisure", "lichess.org": "leisure",
	"org.lichess.mobileapp": "leisure", "rutube.ru": "leisure", "ru.rutube.app": "leisure",
	"vkvideo.ru": "leisure", "com.vk.vkvideo": "leisure", "ivi.ru": "leisure", "ru.ivi.client": "leisure",
	"okko.tv": "leisure", "ru.more.play": "leisure", "kion.ru": "leisure", "ru.mts.mtstv": "leisure",
	"wink.ru": "leisure", "ru.rt.video.app.mobile": "leisure", "start.ru": "leisure", "premier.one": "leisure",
	"gpm.premier": "leisure", "more.tv": "leisure", "hd.kinopoisk.ru": "leisure", "dzen.ru": "leisure",
	"zen.yandex.ru": "leisure", "ru.zen.android": "leisure", "yappy": "leisure", "ru.yappy": "leisure",
	"shikimori.one": "leisure", "anilibria.tv": "leisure", "animego.org": "leisure", "jut.su": "leisure",
	"animevost.org": "leisure", "rezka.ag": "leisure", "hdrezka.ag": "leisure", "lordfilm": "leisure",
	"filmix.ac": "leisure", "kinogo": "leisure", "twitch": "leisure", "music.youtube.com": "leisure",
	"soundcloud.com": "leisure", "com.soundcloud.android": "leisure", "zvuk.com": "leisure",
	"com.zvooq.openplay": "leisure", "ru.mts.music.android": "leisure", "music.mts.ru": "leisure",
	"boom": "leisure", "com.uma.musicvk": "leisure", "apple music": "leisure",
	"com.apple.android.music": "leisure", "deezer.com": "leisure", "deezer.android.app": "leisure",
	"litres.ru": "leisure", "ru.litres.android": "leisure", "bookmate.ru": "leisure", "com.bookmate": "leisure",
	"author.today": "leisure", "ficbook.net": "leisure", "mangalib.me": "leisure", "remanga.org": "leisure",
	"readmanga.live": "leisure", "webtoons.com": "leisure", "com.naver.linewebtoon": "leisure",
	"joyreactor.cc": "leisure", "yaplakal.com": "leisure", "fishki.net": "leisure", "imgur.com": "leisure",
	"9gag": "leisure", "ifunny.co": "leisure", "br.com.ifunny": "leisure", "com.yandex.zen": "leisure",
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
