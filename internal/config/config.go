// Package config loads, validates and describes config.toml.
//
// Struct tags drive validation:
//   - `oneof:"a|b|c"`  value must be one of the listed strings (error, default kept)
//   - `range:"min,max"` integer is clamped into [min,max] (warning)
//
// See docs/specs/04-config-keymap-theme.md.
package config

// Config is the fully validated configuration.
type Config struct {
	General       General       `toml:"general"`
	Playback      Playback      `toml:"playback"`
	Cache         Cache         `toml:"cache"`
	YtDlp         YtDlp         `toml:"yt_dlp"`
	Search        Search        `toml:"search"`
	UI            UI            `toml:"ui"`
	Theme         Theme         `toml:"theme"`
	Keys          Keys          `toml:"keys"`
	Notifications Notifications `toml:"notifications"`
	MPRIS         MPRIS         `toml:"mpris"`
	Discord       Discord       `toml:"discord"`
	Lyrics        Lyrics        `toml:"lyrics"`
	LastFM        LastFM        `toml:"lastfm"`
	Logging       Logging       `toml:"logging"`
}

// General holds [general].
type General struct {
	StartupPage     string `toml:"startup_page" oneof:"library|search|browse|queue|liked_songs|recently_played|help"`
	BrandAccountID  string `toml:"brand_account_id"`
	CheckForUpdates bool   `toml:"check_for_updates"`
}

// Playback holds [playback].
type Playback struct {
	AudioQuality            string   `toml:"audio_quality" oneof:"high|medium|low"`
	DefaultVolume           int      `toml:"default_volume" range:"0,100"`
	Autoplay                bool     `toml:"autoplay"`
	Gapless                 bool     `toml:"gapless"`
	SeekStep                int      `toml:"seek_step" range:"1,300"`
	APITimeout              int      `toml:"api_timeout" range:"1,120"`
	ResumeOnLaunch          bool     `toml:"resume_on_launch"`
	HistoryMinListenSeconds int      `toml:"history_min_listen_seconds" range:"0,3600"`
	SyncHistoryToYTMusic    bool     `toml:"sync_history_to_ytmusic"`
	MPVConfig               bool     `toml:"mpv_config"`
	MPVArgs                 []string `toml:"mpv_args"`
}

// Cache holds [cache].
type Cache struct {
	MaxSizeMB int    `toml:"max_size_mb" range:"0,1048576"`
	Location  string `toml:"location"`
}

// YtDlp holds [yt_dlp].
type YtDlp struct {
	CookiesFile       string `toml:"cookies_file"`
	UseSessionCookies bool   `toml:"use_session_cookies"`
	CookieBrowser     string `toml:"cookie_browser"`
	CABundle          string `toml:"ca_bundle"`
	RemoteComponents  string `toml:"remote_components"`
	JSRuntimes        string `toml:"js_runtimes"`
	Binary            string `toml:"binary"`
}

// Search holds [search].
type Search struct {
	DefaultMode string `toml:"default_mode" oneof:"music|all"`
	MaxHistory  int    `toml:"max_history" range:"0,100000"`
	Predictive  bool   `toml:"predictive"`
}

// UI holds [ui].
type UI struct {
	AlbumArtMode      string `toml:"album_art_mode" oneof:"auto|kitty|blocks|off"`
	Icons             string `toml:"icons" oneof:"unicode|nerd|ascii"`
	ProgressStyle     string `toml:"progress_style" oneof:"block|line"`
	SidebarWidth      int    `toml:"sidebar_width" range:"16,80"`
	LyricsWidth       int    `toml:"lyrics_width" range:"24,80"`
	ColIndex          int    `toml:"col_index" range:"0,20"`
	ColTitle          int    `toml:"col_title" range:"0,200"`
	ColArtist         int    `toml:"col_artist" range:"0,200"`
	ColAlbum          int    `toml:"col_album" range:"0,200"`
	ColDuration       int    `toml:"col_duration" range:"0,20"`
	BidiMode          string `toml:"bidi_mode" oneof:"auto|reorder|passthrough"`
	Region            string `toml:"region"`
	HomeShelves       int    `toml:"home_shelves" range:"1,25"`
	ShowSelectionInfo bool   `toml:"show_selection_info"`
	SidebarOverflow   string `toml:"sidebar_overflow" oneof:"truncate|wrap"`
	ShowQueueSource   bool   `toml:"show_queue_source"`
	Mouse             bool   `toml:"mouse"`
	// ShowSidebar shows the playlist sidebar at start (spec 09).
	ShowSidebar bool `toml:"show_sidebar"`
	// ShowHints shows the key-hint footer (spec 09).
	ShowHints bool `toml:"show_hints"`
}

// Theme holds [theme]. See docs/specs/08-dynamic-theme-transparency.md.
type Theme struct {
	Source               string            `toml:"source" oneof:"static|pywal|matugen|ansi|file"`
	Name                 string            `toml:"name"`
	Transparent          bool              `toml:"transparent"`
	TransparentSelection string            `toml:"transparent_selection" oneof:"bar|reverse|underline|solid"`
	PopupFill            bool              `toml:"popup_fill"`
	EnforceContrast      bool              `toml:"enforce_contrast"`
	Watch                bool              `toml:"watch"`
	PywalPath            string            `toml:"pywal_path"`
	MatugenPath          string            `toml:"matugen_path"`
	File                 string            `toml:"file"`
	Fallback             string            `toml:"fallback"`
	ColorDepth           string            `toml:"color_depth" oneof:"auto|truecolor|256|16"`
	ANSIAccent           int               `toml:"ansi_accent" range:"0,15"`
	PywalMap             map[string]string `toml:"pywal_map"`
}

// Keys holds [keys] settings in config.toml (bindings live in keymap.toml).
type Keys struct {
	ChordTimeoutMS int `toml:"chord_timeout_ms" range:"100,5000"`
}

// Notifications holds [notifications].
type Notifications struct {
	Enabled        bool   `toml:"enabled"`
	TimeoutSeconds int    `toml:"timeout_seconds" range:"1,60"`
	Format         string `toml:"format"`
}

// MPRIS holds [mpris].
type MPRIS struct {
	Enabled bool `toml:"enabled"`
}

// Discord holds [discord].
type Discord struct {
	Enabled  bool   `toml:"enabled"`
	ClientID string `toml:"client_id"`
}

// Lyrics holds [lyrics].
type Lyrics struct {
	Transliteration bool `toml:"transliteration"`
}

// LastFM holds [lastfm].
type LastFM struct {
	Enabled      bool   `toml:"enabled"`
	APIKey       string `toml:"api_key"`
	APISecret    string `toml:"api_secret"`
	SessionKey   string `toml:"session_key"`
	Username     string `toml:"username"`
	PasswordHash string `toml:"password_hash"`
}

// Logging holds [logging].
type Logging struct {
	Level       string `toml:"level" oneof:"DEBUG|INFO|WARNING|ERROR|CRITICAL"`
	MaxBytes    int    `toml:"max_bytes" range:"65536,1073741824"`
	BackupCount int    `toml:"backup_count" range:"0,50"`
	KeepCrashes int    `toml:"keep_crashes" range:"0,1000"`
}

// CookieBrowsers lists the accepted [yt_dlp] cookie_browser values.
var CookieBrowsers = []string{"helium", "chrome", "chromium", "brave", "firefox", "zen", "edge", "vivaldi", "opera"}

// Default returns the built-in configuration. It is the single source of
// truth for defaults (printed by `ytm config defaults`).
func Default() Config {
	return Config{
		General: General{StartupPage: "library", CheckForUpdates: true},
		Playback: Playback{
			AudioQuality: "high", DefaultVolume: 80, Autoplay: true, Gapless: true,
			SeekStep: 5, APITimeout: 15, ResumeOnLaunch: true, HistoryMinListenSeconds: 5,
			SyncHistoryToYTMusic: true, MPVArgs: []string{},
		},
		Cache:  Cache{MaxSizeMB: 1024},
		Search: Search{DefaultMode: "music", MaxHistory: 500, Predictive: true},
		UI: UI{
			AlbumArtMode: "auto", Icons: "unicode", ProgressStyle: "block", SidebarWidth: 30,
			LyricsWidth: 36, ColIndex: 4, ColDuration: 8, BidiMode: "auto", Region: "ZZ",
			HomeShelves: 3, ShowSelectionInfo: true, SidebarOverflow: "truncate",
			ShowQueueSource: true, Mouse: true, ShowHints: true,
		},
		Theme: Theme{
			Source: "static", Name: "ytm-dark", TransparentSelection: "solid", EnforceContrast: true,
			Watch: true, Fallback: "ytm-dark", ColorDepth: "auto", ANSIAccent: 4,
			PywalMap: map[string]string{},
		},
		Keys:          Keys{ChordTimeoutMS: 1000},
		Notifications: Notifications{Enabled: true, TimeoutSeconds: 5, Format: "{title} — {artist}"},
		MPRIS:         MPRIS{Enabled: true},
		Logging:       Logging{Level: "WARNING", MaxBytes: 5 << 20, BackupCount: 3, KeepCrashes: 10},
	}
}
