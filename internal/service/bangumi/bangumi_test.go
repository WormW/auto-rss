package bangumi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBangumiService_GetSubject(t *testing.T) {
	tests := []struct {
		name       string
		subjectID  int
		response   string
		statusCode int
		wantErr    bool
		wantName   string
	}{
		{
			name:       "success",
			subjectID:  123,
			response:   `{"id": 123, "name": "Test Anime", "name_cn": "测试动画", "eps": 12}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantName:   "Test Anime",
		},
		{
			name:       "not found",
			subjectID:  999,
			response:   `{"error": "Not Found"}`,
			statusCode: http.StatusNotFound,
			wantErr:    true,
			wantName:   "",
		},
		{
			name:       "server error",
			subjectID:  456,
			response:   `{"error": "Internal Server Error"}`,
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
			wantName:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/v0/subjects/"
				assert.Contains(t, r.URL.Path, expectedPath)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			service := NewBangumiService()
			service.baseURL = server.URL

			subject, err := service.GetSubject(tt.subjectID)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, subject.Name)
		})
	}
}

func TestBangumiService_Search(t *testing.T) {
	tests := []struct {
		name      string
		keyword   string
		response  string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "success with results",
			keyword:   "anime",
			response:  `{"total": 2, "data": [{"id": 1, "name": "Anime 1"}, {"id": 2, "name": "Anime 2"}]}`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "empty results",
			keyword:   "xyznotfound",
			response:  `{"total": 0, "data": []}`,
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/search/subjects")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			service := NewBangumiService()
			service.baseURL = server.URL

			result, err := service.Search(tt.keyword, SubjectTypeAnime)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result.List, tt.wantCount)
		})
	}
}

func TestBangumiService_GetSubjectEpisodes(t *testing.T) {
	tests := []struct {
		name      string
		subjectID int
		response  string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "success with episodes",
			subjectID: 123,
			response: `{
				"data": [
					{"id": 1, "type": 0, "sort": 1, "name": "Episode 1"},
					{"id": 2, "type": 0, "sort": 2, "name": "Episode 2"}
				],
				"total": 2
			}`,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "no episodes",
			subjectID: 456,
			response:  `{"data": [], "total": 0}`,
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/v0/episodes")
				assert.Equal(t, fmt.Sprintf("%d", tt.subjectID), r.URL.Query().Get("subject_id"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			service := NewBangumiService()
			service.baseURL = server.URL

			episodes, err := service.GetSubjectEpisodes(tt.subjectID)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, episodes, tt.wantCount)
		})
	}
}

func TestBangumiService_SetProxy(t *testing.T) {
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
			service := NewBangumiService()

			err := service.SetProxy(tt.proxyURL)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBangumiService_SearchByName(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantErr  bool
	}{
		{
			name:     "success finds result",
			response: `{"total": 1, "data": [{"id": 123, "name": "Test Anime", "name_cn": "测试动画"}]}`,
			wantErr:  false,
		},
		{
			name:     "no results",
			response: `{"total": 0, "data": []}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			service := NewBangumiService()
			service.baseURL = server.URL

			subject, err := service.SearchByName("test")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, subject)
		})
	}
}
