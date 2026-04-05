package mikan

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMikanService_Search(t *testing.T) {
	tests := []struct {
		name      string
		search    string
		html      string
		wantCount int
		wantErr   bool
	}{
		{
			name:   "success with results",
			search: "anime",
			html: `
<!DOCTYPE html>
<html>
<body>
	<div class="an-ul">
		<li>
			<span data-src="/cover1.jpg"></span>
			<a href="/Home/Anime/123">Anime Title 1</a>
		</li>
		<li>
			<span data-src="/cover2.jpg"></span>
			<a href="/Home/Anime/456">Anime Title 2</a>
		</li>
	</div>
</body>
</html>`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:   "no results",
			search: "xyznotfound",
			html: `
<!DOCTYPE html>
<html>
<body>
	<div class="an-ul">
		<p>No results found</p>
	</div>
</body>
</html>`,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "server error",
			search:    "error",
			html:      "",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.wantErr && tt.name == "server error" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				assert.Contains(t, r.URL.Path, "/Home/Search")
				assert.Equal(t, tt.search, r.URL.Query().Get("searchstr"))
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.html))
			}))
			defer server.Close()

			service := NewMikanService("")
			service.baseURL = server.URL

			result, err := service.Search(tt.search)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			require.GreaterOrEqual(t, len(result.Groups), 1)
			assert.Len(t, result.Groups[0].Items, tt.wantCount)
		})
	}
}

func TestMikanService_GetBySeason(t *testing.T) {
	tests := []struct {
		name      string
		year      int
		season    string
		html      string
		wantCount int
		wantErr   bool
	}{
		{
			name:   "success with bangumi groups",
			year:   2024,
			season: "Winter",
			html: `
<!DOCTYPE html>
<html>
<body>
	<div class="sk-bangumi">
		<div>Monday</div>
		<div class="an-ul">
			<li>
				<span data-src="/cover1.jpg"></span>
				<a href="/Home/Anime/123">Anime 1</a>
			</li>
		</div>
	</div>
	<div class="sk-bangumi">
		<div>Tuesday</div>
		<div class="an-ul">
			<li>
				<span data-src="/cover2.jpg"></span>
				<a href="/Home/Anime/456">Anime 2</a>
			</li>
		</div>
	</div>
</body>
</html>`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "server error",
			year:      2024,
			season:    "Winter",
			html:      "",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.wantErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				assert.Contains(t, r.URL.Path, "/Home/BangumiCoverFlowByDayOfWeek")
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.html))
			}))
			defer server.Close()

			service := NewMikanService("")
			service.baseURL = server.URL

			result, err := service.GetBySeason(tt.year, tt.season)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			// Count total items across all groups
			totalItems := 0
			for _, group := range result.Groups {
				totalItems += len(group.Items)
			}
			assert.Equal(t, tt.wantCount, totalItems)
		})
	}
}

func TestMikanService_GetFansubGroups(t *testing.T) {
	tests := []struct {
		name      string
		animeURL  string
		html      string
		wantCount int
		wantErr   bool
	}{
		{
			name:     "success with fansub groups",
			animeURL: "/Home/Anime/123",
			html: `
<!DOCTYPE html>
<html>
<body>
	<div class="leftbar-item">
		<a class="subgroup-name" data-anchor="#subgroup-1">Group 1</a>
		<div class="date">Monday</div>
	</div>
	<div id="subgroup-1">
		<a class="mikan-rss" href="/RSS/Anime/123/1">RSS</a>
		<table>
			<tbody>
				<tr>
					<td><a>Episode 1 - 第01集 [1080P][简体]</a></td>
				</tr>
			</tbody>
		</table>
	</div>
</body>
</html>`,
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "not found",
			animeURL:  "/Home/Anime/999",
			html:      `<html><body>Not Found</body></html>`,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "server error",
			animeURL:  "/Home/Anime/456",
			html:      "",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.wantErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.html))
			}))
			defer server.Close()

			service := NewMikanService("")
			service.baseURL = server.URL

			groups, err := service.GetFansubGroups(server.URL + tt.animeURL)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, groups, tt.wantCount)
		})
	}
}

func TestMikanService_SetProxy(t *testing.T) {
	tests := []struct {
		name      string
		proxyURL  string
		wantError bool
	}{
		{
			name:      "empty proxy clears transport",
			proxyURL:  "",
			wantError: false,
		},
		{
			name:      "valid proxy URL",
			proxyURL:  "http://proxy.example.com:8080",
			wantError: false,
		},
		{
			name:      "invalid proxy URL",
			proxyURL:  "://invalid-url",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewMikanService("")

			err := service.SetProxy(tt.proxyURL)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExtractTags(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		expected []string
	}{
		{
			name:     "1080P resolution",
			title:    "[SubGroup] Anime Title - 01 [1080P][简体].mp4",
			expected: []string{"1080P", "简体", "MP4"},
		},
		{
			name:     "720P with traditional Chinese",
			title:    "[SubGroup] Anime Title - 01 [720p][繁体].mkv",
			expected: []string{"720P", "繁体", "MKV"},
		},
		{
			name:     "4K resolution",
			title:    "[SubGroup] Anime Title - 01 [4K].avi",
			expected: []string{"4K", "AVI"},
		},
		{
			name:     "no tags",
			title:    "Anime Title",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := extractTags(tt.title)
			assert.Equal(t, tt.expected, tags)
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "contains item",
			slice:    []string{"a", "b", "c"},
			item:     "b",
			expected: true,
		},
		{
			name:     "does not contain item",
			slice:    []string{"a", "b", "c"},
			item:     "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			item:     "a",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.slice, tt.item)
			assert.Equal(t, tt.expected, result)
		})
	}
}
