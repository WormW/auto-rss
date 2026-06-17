package downloader

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/pkg/logger"
)

const tvShowNFOFileName = "tvshow.nfo"

type tvShowNFO struct {
	XMLName       xml.Name       `xml:"tvshow"`
	Title         string         `xml:"title,omitempty"`
	OriginalTitle string         `xml:"originaltitle,omitempty"`
	Plot          string         `xml:"plot,omitempty"`
	Premiered     string         `xml:"premiered,omitempty"`
	Year          int            `xml:"year,omitempty"`
	Rating        float64        `xml:"rating,omitempty"`
	Thumb         string         `xml:"thumb,omitempty"`
	BangumiID     string         `xml:"bangumiid"`
	UniqueID      tvShowUniqueID `xml:"uniqueid"`
}

type tvShowUniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

type tvShowBangumiIDElement struct {
	XMLName xml.Name `xml:"bangumiid"`
	Value   string   `xml:",chardata"`
}

type tvShowUniqueIDElement struct {
	XMLName xml.Name `xml:"uniqueid"`
	Type    string   `xml:"type,attr"`
	Default bool     `xml:"default,attr"`
	Value   string   `xml:",chardata"`
}

// ensureTVShowNFOForRenamedPath writes tvshow.nfo in the show root inferred from
// the final renamed video path. A Season directory is never treated as the root.
func ensureTVShowNFOForRenamedPath(renamedPath string, subscription *model.Subscription) error {
	showRoot := tvShowRootFromRenamedPath(renamedPath)
	return ensureTVShowNFO(showRoot, subscription)
}

func ensureTVShowNFO(showRoot string, subscription *model.Subscription) error {
	if subscription == nil {
		return fmt.Errorf("subscription is nil")
	}
	if subscription.BangumiID <= 0 {
		logger.Info("Skipping tvshow.nfo generation because subscription has no Bangumi ID",
			"subscription_id", subscription.ID,
			"subscription", subscription.Name,
			"show_root", showRoot)
		return nil
	}
	if showRoot == "" || showRoot == "." {
		return fmt.Errorf("invalid show root for tvshow.nfo: %q", showRoot)
	}

	if err := os.MkdirAll(showRoot, 0755); err != nil {
		return fmt.Errorf("create show root for tvshow.nfo: %w", err)
	}

	nfoPath := filepath.Join(showRoot, tvShowNFOFileName)
	if _, err := os.Stat(nfoPath); err == nil {
		return ensureBangumiIdentifiersInExistingNFO(nfoPath, subscription.BangumiID)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat tvshow.nfo: %w", err)
	}

	content, err := buildTVShowNFO(subscription)
	if err != nil {
		return err
	}
	if err := os.WriteFile(nfoPath, content, 0644); err != nil {
		return fmt.Errorf("write tvshow.nfo: %w", err)
	}

	logger.Info("Generated tvshow.nfo",
		"subscription_id", subscription.ID,
		"bangumi_id", subscription.BangumiID,
		"path", nfoPath)
	return nil
}

func buildTVShowNFO(subscription *model.Subscription) ([]byte, error) {
	bangumiID := fmt.Sprintf("%d", subscription.BangumiID)
	nfo := tvShowNFO{
		Title:         subscription.Name,
		OriginalTitle: subscription.Name,
		Plot:          subscription.BangumiSummary,
		Premiered:     subscription.AirDate,
		Year:          subscription.AirYear,
		Rating:        subscription.BangumiScore,
		Thumb:         firstNonEmptyString(subscription.BangumiCoverLocal, subscription.BangumiCover),
		BangumiID:     bangumiID,
		UniqueID: tvShowUniqueID{
			Type:    "bangumi",
			Default: true,
			Value:   bangumiID,
		},
	}

	body, err := xml.MarshalIndent(nfo, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal tvshow.nfo: %w", err)
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

func ensureBangumiIdentifiersInExistingNFO(nfoPath string, bangumiID int) error {
	content, err := os.ReadFile(nfoPath)
	if err != nil {
		return fmt.Errorf("read existing tvshow.nfo: %w", err)
	}

	hasBangumiID, hasBangumiUniqueID, err := inspectTVShowNFOIdentifiers(content)
	if err != nil {
		return fmt.Errorf("inspect existing tvshow.nfo: %w", err)
	}
	if hasBangumiID && hasBangumiUniqueID {
		return nil
	}

	fragment, err := buildMissingBangumiIdentifierFragment(bangumiID, !hasBangumiID, !hasBangumiUniqueID)
	if err != nil {
		return err
	}

	closingTag := []byte("</tvshow>")
	closingIndex := bytes.LastIndex(bytes.ToLower(content), closingTag)
	if closingIndex < 0 {
		return fmt.Errorf("existing tvshow.nfo has no closing tvshow element")
	}

	var updated bytes.Buffer
	updated.Write(content[:closingIndex])
	if len(content[:closingIndex]) > 0 && !bytes.HasSuffix(content[:closingIndex], []byte("\n")) {
		updated.WriteByte('\n')
	}
	updated.Write(fragment)
	updated.Write(content[closingIndex:])

	if err := os.WriteFile(nfoPath, updated.Bytes(), 0644); err != nil {
		return fmt.Errorf("update existing tvshow.nfo: %w", err)
	}

	logger.Info("Updated tvshow.nfo with missing Bangumi identifiers",
		"bangumi_id", bangumiID,
		"path", nfoPath,
		"added_bangumiid", !hasBangumiID,
		"added_uniqueid", !hasBangumiUniqueID)
	return nil
}

func inspectTVShowNFOIdentifiers(content []byte) (bool, bool, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var root string
	var hasBangumiID bool
	var hasBangumiUniqueID bool

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return false, false, err
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}

		if root == "" {
			root = start.Name.Local
			if root != "tvshow" {
				return false, false, fmt.Errorf("root element is %q, want tvshow", root)
			}
		}

		switch start.Name.Local {
		case "bangumiid":
			value, err := readElementText(decoder, start.Name)
			if err != nil {
				return false, false, err
			}
			if strings.TrimSpace(value) != "" {
				hasBangumiID = true
			}
		case "uniqueid":
			if hasXMLAttr(start, "type", "bangumi") {
				value, err := readElementText(decoder, start.Name)
				if err != nil {
					return false, false, err
				}
				if strings.TrimSpace(value) != "" {
					hasBangumiUniqueID = true
				}
			}
		}
	}

	if root == "" {
		return false, false, fmt.Errorf("empty tvshow.nfo")
	}
	return hasBangumiID, hasBangumiUniqueID, nil
}

func readElementText(decoder *xml.Decoder, name xml.Name) (string, error) {
	var text strings.Builder
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if t.Name.Local == name.Local {
				depth--
			}
		case xml.CharData:
			text.Write([]byte(t))
		}
	}
	return text.String(), nil
}

func buildMissingBangumiIdentifierFragment(bangumiID int, includeBangumiID bool, includeUniqueID bool) ([]byte, error) {
	var fragment bytes.Buffer
	bangumiIDValue := fmt.Sprintf("%d", bangumiID)

	if includeBangumiID {
		body, err := xml.MarshalIndent(tvShowBangumiIDElement{Value: bangumiIDValue}, "  ", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal bangumiid: %w", err)
		}
		fragment.Write(body)
		fragment.WriteByte('\n')
	}
	if includeUniqueID {
		body, err := xml.MarshalIndent(tvShowUniqueIDElement{Type: "bangumi", Default: true, Value: bangumiIDValue}, "  ", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal bangumi uniqueid: %w", err)
		}
		fragment.Write(body)
		fragment.WriteByte('\n')
	}

	return fragment.Bytes(), nil
}

func tvShowRootFromRenamedPath(renamedPath string) string {
	dir := filepath.Dir(renamedPath)
	if isSeasonDirectory(filepath.Base(dir)) {
		return filepath.Dir(dir)
	}
	return dir
}

func isSeasonDirectory(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(normalized, "season ") || strings.HasPrefix(normalized, "season_") || strings.HasPrefix(normalized, "season-")
}

func hasXMLAttr(start xml.StartElement, name string, value string) bool {
	for _, attr := range start.Attr {
		if attr.Name.Local == name && attr.Value == value {
			return true
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
