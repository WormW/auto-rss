package downloader

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
)

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
