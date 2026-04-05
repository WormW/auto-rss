package organizer

import (
	"strings"
	"testing"
)

func TestFileNameParser_Parse(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     *FileNameInfo
	}{
		{
			name:     "Standard anime filename",
			filename: "[LoliHouse] Princess-Session Orchestra - 01 [WebRip 1080p HEVC-10bit AAC].mkv",
			want: &FileNameInfo{
				OriginalName: "[LoliHouse] Princess-Session Orchestra - 01 [WebRip 1080p HEVC-10bit AAC].mkv",
				Title:        "Princess-Session Orchestra",
				Episode:      1,
				Season:       1,
				Fansub:       "LoliHouse",
				Resolution:   "1080p",
				Language:     "",
				VideoCodec:   "HEVC",
				AudioCodec:   "AAC",
				Extension:    ".mkv",
				Quality:      "WebRip",
			},
		},
		{
			name:     "Filename with language tag",
			filename: "[Group] Title - 03 [CHS][1080p].mkv",
			want: &FileNameInfo{
				OriginalName: "[Group] Title - 03 [CHS][1080p].mkv",
				Title:        "Title",
				Episode:      3,
				Season:       1,
				Fansub:       "Group",
				Resolution:   "1080p",
				Language:     "CHS",
				Extension:    ".mkv",
			},
		},
		{
			name:     "Filename with BDRip quality",
			filename: "[Group] Movie Name [BDRip 1080p H264 FLAC].mkv",
			want: &FileNameInfo{
				OriginalName: "[Group] Movie Name [BDRip 1080p H264 FLAC].mkv",
				Title:        "Movie Name",
				Episode:      0,
				Season:       1,
				Fansub:       "Group",
				Resolution:   "1080p",
				VideoCodec:   "AVC",
				AudioCodec:   "FLAC",
				Extension:    ".mkv",
				Quality:      "BDRip",
			},
		},
		{
			name:     "Filename with multiple info blocks",
			filename: "[Fansub] Anime Name - 08 [WebRip][1080p][HEVC][CHS].mkv",
			want: &FileNameInfo{
				OriginalName: "[Fansub] Anime Name - 08 [WebRip][1080p][HEVC][CHS].mkv",
				Title:        "Anime Name",
				Episode:      8,
				Season:       1,
				Fansub:       "Fansub",
				Resolution:   "1080p",
				Language:     "CHS",
				VideoCodec:   "HEVC",
				Extension:    ".mkv",
				Quality:      "WebRip",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.Parse(tt.filename)

			if got.OriginalName != tt.want.OriginalName {
				t.Errorf("OriginalName = %v, want %v", got.OriginalName, tt.want.OriginalName)
			}
			if got.Title != tt.want.Title {
				t.Errorf("Title = %v, want %v", got.Title, tt.want.Title)
			}
			if got.Episode != tt.want.Episode {
				t.Errorf("Episode = %v, want %v", got.Episode, tt.want.Episode)
			}
			if got.Season != tt.want.Season {
				t.Errorf("Season = %v, want %v", got.Season, tt.want.Season)
			}
			if got.Fansub != tt.want.Fansub {
				t.Errorf("Fansub = %v, want %v", got.Fansub, tt.want.Fansub)
			}
			if got.Resolution != tt.want.Resolution {
				t.Errorf("Resolution = %v, want %v", got.Resolution, tt.want.Resolution)
			}
			if got.Language != tt.want.Language {
				t.Errorf("Language = %v, want %v", got.Language, tt.want.Language)
			}
			if got.VideoCodec != tt.want.VideoCodec {
				t.Errorf("VideoCodec = %v, want %v", got.VideoCodec, tt.want.VideoCodec)
			}
			if got.AudioCodec != tt.want.AudioCodec {
				t.Errorf("AudioCodec = %v, want %v", got.AudioCodec, tt.want.AudioCodec)
			}
			if got.Extension != tt.want.Extension {
				t.Errorf("Extension = %v, want %v", got.Extension, tt.want.Extension)
			}
			if got.Quality != tt.want.Quality {
				t.Errorf("Quality = %v, want %v", got.Quality, tt.want.Quality)
			}
		})
	}
}

func TestFileNameParser_extractFansub(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"With fansub", "[LoliHouse] Title.mkv", "LoliHouse"},
		{"Without fansub", "Title.mkv", ""},
		{"Empty brackets", "[] Title.mkv", ""},
		{"Multiple brackets", "[Group][Tag] Title.mkv", "Group"},
		{"Fansub with spaces", "[Some Group] Title.mkv", "Some Group"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractFansub(tt.filename)
			if got != tt.want {
				t.Errorf("extractFansub() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractEpisode(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     int
	}{
		{"Episode prefix uppercase", "Title EP12.mkv", 12},
		{"Episode prefix lowercase", "Title ep5.mkv", 5},
		{"Chinese episode marker", "Title 第5集.mkv", 5},
		{"Chinese hua marker", "Title 第3话.mkv", 3},
		{"SxxExx format", "Title S01E08.mkv", 8},
		{"Episode with dot", "Title Ep.15.mkv", 15},
		{"No episode", "Title.mkv", 0},
		{"Episode 0", "Title - 00.mkv", 0},
		{"Episode in brackets at end", "Title - 12 [1080p].mkv", 12},
		{"Standard format with space dash", "Title - 05 - [Tag].mkv", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractEpisode(tt.filename)
			if got != tt.want {
				t.Errorf("extractEpisode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractSeason(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     int
	}{
		{"S01 format", "Title S01E01.mkv", 1},
		{"S2 format", "Title S2E05.mkv", 2},
		{"Season prefix", "Title Season 3 Episode 1.mkv", 3},
		{"Chinese season", "Title 第2季.mkv", 2},
		{"No season", "Title - 01.mkv", 0},
		{"S10 format", "Title S10E01.mkv", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractSeason(tt.filename)
			if got != tt.want {
				t.Errorf("extractSeason() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractResolution(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"1080p", "Title [1080p].mkv", "1080p"},
		{"720p", "Title 720p.mkv", "720p"},
		{"4K", "Title 4K.mkv", "4K"},
		{"2160p", "Title [2160p].mkv", "2160p"},
		{"UHD", "Title UHD.mkv", "UHD"},
		{"480p lowercase", "title 480p.mkv", "480p"},
		{"No resolution", "Title.mkv", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractResolution(tt.filename)
			if got != tt.want {
				t.Errorf("extractResolution() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractLanguage(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"CHS", "Title [CHS].mkv", "CHS"},
		{"CHT", "Title CHT.mkv", "CHT"},
		{"Simplified Chinese", "Title 简中.mkv", "CHS"},
		{"Traditional Chinese", "Title 繁中.mkv", "CHT"},
		{"GB", "Title GB.mkv", "CHS"},
		{"BIG5", "Title BIG5.mkv", "CHT"},
		{"No language", "Title.mkv", ""},
		{"Mixed case", "title [chs].mkv", "CHS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractLanguage(tt.filename)
			if got != tt.want {
				t.Errorf("extractLanguage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractVideoCodec(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"HEVC", "Title [HEVC].mkv", "HEVC"},
		{"H.265", "Title H.265.mkv", "HEVC"},
		{"x265", "Title x265.mkv", "HEVC"},
		{"H264", "Title H264.mkv", "AVC"},
		{"AVC", "Title [AVC].mkv", "AVC"},
		{"VP9", "Title VP9.mkv", "VP9"},
		{"AV1", "Title AV1.mkv", "AV1"},
		{"No codec", "Title.mkv", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractVideoCodec(tt.filename)
			if got != tt.want {
				t.Errorf("extractVideoCodec() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractAudioCodec(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"FLAC", "Title [FLAC].mkv", "FLAC"},
		{"AAC", "Title AAC.mkv", "AAC"},
		{"AC3", "Title AC3.mkv", "AC3"},
		{"DTS", "Title [DTS].mkv", "DTS"},
		{"OPUS", "Title OPUS.mkv", "OPUS"},
		{"MP3", "Title MP3.mkv", "MP3"},
		{"No audio codec", "Title.mkv", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractAudioCodec(tt.filename)
			if got != tt.want {
				t.Errorf("extractAudioCodec() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractQuality(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"BDRip", "Title [BDRip].mkv", "BDRip"},
		{"WebRip", "Title WebRip.mkv", "WebRip"},
		{"WEB-DL", "Title WEB-DL.mkv", "WEB-DL"},
		{"WEBRip", "Title [WEBRip].mkv", "WebRip"},
		{"BluRay", "Title BluRay.mkv", "BluRay"},
		{"HDTV", "Title HDTV.mkv", "HDTV"},
		{"No quality", "Title.mkv", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractQuality(tt.filename)
			if got != tt.want {
				t.Errorf("extractQuality() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_MatchTitle(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name    string
		title1  string
		title2  string
		wantMin float64
		wantMax float64
	}{
		{"Exact match", "Anime Title", "Anime Title", 1.0, 1.0},
		{"Case insensitive", "Anime Title", "anime title", 1.0, 1.0},
		{"Containment", "Anime Title Season 1", "Anime Title", 0.8, 0.8},
		{"Similar titles", "Princess Session Orchestra", "Princess-Session Orchestra", 0.6, 1.0},
		{"Different titles", "Anime A", "Anime B", 0.0, 0.9},
		{"Empty strings", "", "", 1.0, 1.0},
		{"One empty", "Anime", "", 0.0, 0.9},
		{"With special chars", "Title: Special!", "Title Special", 0.5, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.MatchTitle(tt.title1, tt.title2)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("MatchTitle() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestFileNameParser_normalizeTitle(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"Lowercase", "TITLE", "title"},
		{"Remove special chars", "Title: Special!", "titlespecial"},
		{"Keep letters and digits", "Anime123", "anime123"},
		{"Unicode handling", "番剧Title", "番剧title"},
		{"Mixed content", "Anime-Title_2024!", "animetitle2024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.normalizeTitle(tt.title)
			if got != tt.want {
				t.Errorf("normalizeTitle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_levenshteinDistance(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name string
		s1   string
		s2   string
		want int
	}{
		{"Empty strings", "", "", 0},
		{"One empty", "hello", "", 5},
		{"Exact match", "hello", "hello", 0},
		{"One char diff", "hello", "hallo", 1},
		{"Different lengths", "hello", "hi", 4},
		{"Completely different", "abc", "xyz", 3},
		{"Substring", "hello", "helo", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.levenshteinDistance(tt.s1, tt.s2)
			if got != tt.want {
				t.Errorf("levenshteinDistance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileNameParser_extractTitle(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name     string
		filename string
		info     *FileNameInfo
		want     string
	}{
		{
			name:     "Standard title",
			filename: "[Group] Anime Title - 01 [1080p]",
			info:     &FileNameInfo{Fansub: "Group", Episode: 1},
			want:     "Anime Title",
		},
		{
			name:     "Title with spaces",
			filename: "[Group]  Princess Session Orchestra  - 01",
			info:     &FileNameInfo{Fansub: "Group", Episode: 1},
			want:     "Princess Session Orchestra",
		},
		{
			name:     "Title without fansub",
			filename: "Anime Title - 01",
			info:     &FileNameInfo{Episode: 1},
			want:     "Anime Title",
		},
		{
			name:     "Title without episode",
			filename: "[Group] Anime Title [1080p]",
			info:     &FileNameInfo{Fansub: "Group", Episode: 0},
			want:     "Anime Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.extractTitle(tt.filename, tt.info)
			if strings.TrimSpace(got) != tt.want {
				t.Errorf("extractTitle() = %v, want %v", got, tt.want)
			}
		})
	}
}
