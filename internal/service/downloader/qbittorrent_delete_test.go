package downloader

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
)

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
