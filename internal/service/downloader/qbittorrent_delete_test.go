package downloader

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestQBittorrentClientAddTorrentExclusiveRejectsPreexistingHashWithoutMutation(t *testing.T) {
	const existingHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	addCalls := 0
	categoryCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"hash": existingHash, "name": "existing", "category": "Subscription", "save_path": "/downloads/shared",
			}})
		case "/api/v2/torrents/add":
			addCalls++
			w.WriteHeader(http.StatusOK)
		case "/api/v2/torrents/setCategory":
			categoryCalls++
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client := &qbittorrentClient{host: server.URL, client: resty.New()}

	_, err := client.AddTorrentExclusive(
		"magnet:?xt=urn:btih:"+existingHash,
		"/downloads/shared",
		"AutoRss:replacement:7",
		existingHash,
	)

	if !errors.Is(err, ErrTorrentAlreadyExists) {
		t.Fatalf("error = %v, want ErrTorrentAlreadyExists", err)
	}
	if addCalls != 0 {
		t.Fatalf("add calls = %d, want 0", addCalls)
	}
	if categoryCalls != 0 {
		t.Fatalf("set category calls = %d, want 0", categoryCalls)
	}
}

func TestQBittorrentClientAddTorrentExclusiveOwnsOnlyNewCategoryHash(t *testing.T) {
	const newHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	added := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			w.Header().Set("Content-Type", "application/json")
			if !added {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"hash": newHash, "name": "new", "category": "AutoRss:replacement:8", "save_path": "/downloads/new",
			}})
		case "/api/v2/torrents/add":
			added = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client := &qbittorrentClient{host: server.URL, client: resty.New()}

	hash, err := client.AddTorrentExclusive(
		"magnet:?xt=urn:btih:"+newHash,
		"/downloads/new",
		"AutoRss:replacement:8",
		newHash,
	)

	if err != nil {
		t.Fatalf("exclusive add failed: %v", err)
	}
	if hash != newHash {
		t.Fatalf("hash = %q, want %q", hash, newHash)
	}
}

func TestQBittorrentClientAddTorrentExclusiveRejectsSavePathMismatch(t *testing.T) {
	const newHash = "cccccccccccccccccccccccccccccccccccccccc"
	added := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			w.Header().Set("Content-Type", "application/json")
			if !added {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"hash": newHash, "name": "new", "category": "AutoRss:replacement:9", "save_path": "/downloads/other",
			}})
		case "/api/v2/torrents/add":
			added = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client := &qbittorrentClient{host: server.URL, client: resty.New()}

	hash, err := client.AddTorrentExclusive(
		"magnet:?xt=urn:btih:"+newHash,
		"/downloads/expected",
		"AutoRss:replacement:9",
		newHash,
	)

	if !errors.Is(err, ErrTorrentOwnershipUnconfirmed) {
		t.Fatalf("error = %v, want ErrTorrentOwnershipUnconfirmed", err)
	}
	if hash != "" {
		t.Fatalf("hash = %q, want empty", hash)
	}
}

func TestQBittorrentClientPauseResumeTorrentContracts(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		invoke   func(*qbittorrentClient) error
		wantHash string
	}{
		{
			name: "pause posts torrent hash",
			path: "/api/v2/torrents/pause",
			invoke: func(client *qbittorrentClient) error {
				return client.PauseTorrent("old-hash")
			},
			wantHash: "old-hash",
		},
		{
			name: "resume posts torrent hash",
			path: "/api/v2/torrents/resume",
			invoke: func(client *qbittorrentClient) error {
				return client.ResumeTorrent("old-hash")
			},
			wantHash: "old-hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotHash string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Fatalf("path = %q, want %q", r.URL.Path, tt.path)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatalf("failed to parse form: %v", err)
				}
				gotHash = r.FormValue("hashes")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := &qbittorrentClient{host: server.URL, client: resty.New()}
			if err := tt.invoke(client); err != nil {
				t.Fatalf("torrent control failed: %v", err)
			}
			if gotHash != tt.wantHash {
				t.Fatalf("hashes = %q, want %q", gotHash, tt.wantHash)
			}
		})
	}
}

func TestQBittorrentClientDeleteTorrentModes(t *testing.T) {
	tests := []struct {
		name            string
		delete          func(*qbittorrentClient) error
		wantDeleteFiles string
	}{
		{
			name: "remove task only keeps payload files",
			delete: func(client *qbittorrentClient) error {
				return client.RemoveTorrentTask("safe-hash")
			},
			wantDeleteFiles: "false",
		},
		{
			name: "delete with payload explicitly deletes files",
			delete: func(client *qbittorrentClient) error {
				return client.DeleteTorrentWithPayload("danger-hash")
			},
			wantDeleteFiles: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotDeleteFiles string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v2/torrents/delete" {
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatalf("failed to parse form: %v", err)
				}
				gotDeleteFiles = r.FormValue("deleteFiles")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := &qbittorrentClient{
				host:   server.URL,
				client: resty.New(),
			}

			if err := tt.delete(client); err != nil {
				t.Fatalf("delete failed: %v", err)
			}

			if gotDeleteFiles != tt.wantDeleteFiles {
				t.Fatalf("deleteFiles = %q, want %q", gotDeleteFiles, tt.wantDeleteFiles)
			}
		})
	}
}
