package rss

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDMHYMagnetIdentityAndSentinelSize(t *testing.T) {
	data, err := os.ReadFile("testdata/dmhy.xml")
	require.NoError(t, err)
	items, err := NewParser().Parse(bytes.NewReader(data))
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "3ca0e670c7e085619cd91a6d07c5938490c767b6", items[0].TorrentHash)
	require.Equal(t, "1df62f6855d5af6412f194b35e26cf40bf79c567", items[1].TorrentHash)
	require.True(t, strings.HasPrefix(items[0].TorrentURL, "magnet:?"), "use the enclosure, not the release HTML page")
	require.Zero(t, items[0].SizeBytes, "DMHY's length=1 is not a real one-byte payload")
	require.Equal(t, 1, items[0].Episode)
	require.False(t, items[0].Trusted)
	for _, replacement := range []string{"https://files.example.test/video.torrent", "magnet:?xt=urn:btih:HSQOM4GH4CCWDHGZDJWQPRMTQSIMOZ5W"} {
		feed := `<rss version="2.0"><channel><title>Other feed</title><item><title>Anime - 01</title><link>https://other.example.test/1</link><enclosure url="` + replacement + `" length="1" /></item></channel></rss>`
		other, err := NewParser().Parse(strings.NewReader(feed))
		require.NoError(t, err)
		require.EqualValues(t, 1, other[0].SizeBytes, "do not relax other feeds' small-resource guard")
	}
}
