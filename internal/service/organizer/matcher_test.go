package organizer

import (
	"errors"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"gorm.io/gorm"
)

// MockSubscriptionRepository 模拟订阅仓库
type MockSubscriptionRepository struct {
	subscriptions []model.Subscription
	listErr       error
}

func (m *MockSubscriptionRepository) Create(subscription *model.Subscription) error {
	return nil
}

func (m *MockSubscriptionRepository) Update(subscription *model.Subscription) error {
	return nil
}

func (m *MockSubscriptionRepository) Delete(id uint) error {
	return nil
}

func (m *MockSubscriptionRepository) GetByID(id uint) (*model.Subscription, error) {
	for i := range m.subscriptions {
		if m.subscriptions[i].ID == id {
			return &m.subscriptions[i], nil
		}
	}
	return nil, errors.New("not found")
}

func (m *MockSubscriptionRepository) GetByRSSURL(rssURL string) (*model.Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) List(offset, limit int) ([]model.Subscription, int64, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.subscriptions, int64(len(m.subscriptions)), nil
}

func (m *MockSubscriptionRepository) GetActiveSubscriptions() ([]model.Subscription, error) {
	return m.subscriptions, nil
}

func (m *MockSubscriptionRepository) UpdateInTx(tx *gorm.DB, subscription *model.Subscription) error {
	return nil
}

func (m *MockSubscriptionRepository) GetSubscriptionsWithDownloadCount() ([]repository.SubscriptionWithStats, error) {
	return nil, nil
}

func TestSubscriptionMatcher_Match(t *testing.T) {
	parser := NewFileNameParser()

	tests := []struct {
		name          string
		info          *FileNameInfo
		subscriptions []model.Subscription
		wantMatch     bool
		wantScoreMin  float64
	}{
		{
			name: "Match should find best matching subscription by title similarity",
			info: &FileNameInfo{
				Title:   "Princess Session Orchestra",
				Episode: 1,
			},
			subscriptions: []model.Subscription{
				{ID: 1, Name: "Princess Session Orchestra"},
				{ID: 2, Name: "Different Anime"},
			},
			wantMatch:    true,
			wantScoreMin: 0.9,
		},
		{
			name: "Match should boost score when fansub matches",
			info: &FileNameInfo{
				Title:   "Princess Session Orchestra",
				Episode: 1,
				Fansub:  "LoliHouse",
			},
			subscriptions: []model.Subscription{
				{ID: 1, Name: "Princess Session Orchestra", Fansub: "LoliHouse"},
				{ID: 2, Name: "Princess Session Orchestra", Fansub: "OtherGroup"},
			},
			wantMatch:    true,
			wantScoreMin: 1.0, // exact match + fansub bonus
		},
		{
			name: "Match should return nil if best score below minimum threshold",
			info: &FileNameInfo{
				Title:   "Completely Different Anime",
				Episode: 1,
			},
			subscriptions: []model.Subscription{
				{ID: 1, Name: "Princess Session Orchestra"},
				{ID: 2, Name: "Another Show"},
			},
			wantMatch:    false,
			wantScoreMin: 0,
		},
		{
			name: "Match should handle empty subscription list gracefully",
			info: &FileNameInfo{
				Title:   "Some Anime",
				Episode: 1,
			},
			subscriptions: []model.Subscription{},
			wantMatch:     false,
			wantScoreMin:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := &MockSubscriptionRepository{
				subscriptions: tt.subscriptions,
			}

			matcher := NewSubscriptionMatcher(parser, mockRepo, nil)

			got, score := matcher.Match(tt.info)

			if tt.wantMatch {
				if got == nil {
					t.Errorf("Match() returned nil, want match")
				} else if score < tt.wantScoreMin {
					t.Errorf("Match() score = %v, want >= %v", score, tt.wantScoreMin)
				}
			} else {
				if got != nil {
					t.Errorf("Match() returned match, want nil")
				}
			}
		})
	}
}


func TestSubscriptionMatcher_SetMinMatchScore(t *testing.T) {
	parser := NewFileNameParser()
	mockRepo := &MockSubscriptionRepository{
		subscriptions: []model.Subscription{
			{ID: 1, Name: "Anime Title"},
		},
	}

	matcher := NewSubscriptionMatcher(parser, mockRepo, nil)

	// Test with default threshold (0.7) - should match
	info := &FileNameInfo{Title: "Anime Title", Episode: 1}
	match, score := matcher.Match(info)
	if match == nil || score < 0.7 {
		t.Errorf("Expected match with default threshold")
	}

	// Set higher threshold (0.95) - should still match exact
	matcher.SetMinMatchScore(0.95)
	match, score = matcher.Match(info)
	if match == nil || score < 0.95 {
		t.Errorf("Expected match with higher threshold for exact match")
	}

	// Set very high threshold (1.0) - exact match should still work
	matcher.SetMinMatchScore(1.0)
	match, score = matcher.Match(info)
	if match == nil {
		t.Errorf("Expected exact match to work with threshold 1.0")
	}
}

func TestSubscriptionMatcher_Match_ListError(t *testing.T) {
	parser := NewFileNameParser()
	mockRepo := &MockSubscriptionRepository{
		listErr: errors.New("database error"),
	}

	matcher := NewSubscriptionMatcher(parser, mockRepo, nil)
	info := &FileNameInfo{Title: "Anime", Episode: 1}

	match, score := matcher.Match(info)
	if match != nil {
		t.Errorf("Expected nil match when list fails")
	}
	if score != 0 {
		t.Errorf("Expected score 0 when list fails, got %v", score)
	}
}

func TestSanitizeDirectoryName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"No special chars", "Normal Name", "Normal Name"},
		{"With slash", "Name/With/Slash", "Name_With_Slash"},
		{"With backslash", "Name\\With\\Backslash", "Name_With_Backslash"},
		{"With colon", "Name: With Colon", "Name_ With Colon"},
		{"With asterisk", "Name*With*Asterisk", "Name_With_Asterisk"},
		{"With question", "Name?With?Question", "Name_With_Question"},
		{"With quote", `Name"With"Quote`, "Name_With_Quote"},
		{"With less than", "Name<With<Less", "Name_With_Less"},
		{"With greater than", "Name>With>Greater", "Name_With_Greater"},
		{"With pipe", "Name|With|Pipe", "Name_With_Pipe"},
		{"Multiple special", `Name:/\*?"<>|`, "Name_________"},
		{"With spaces", "  Name With Spaces  ", "Name With Spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeDirectoryName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeDirectoryName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSimilarDirectoryName(t *testing.T) {
	tests := []struct {
		name  string
		name1 string
		name2 string
		want  bool
	}{
		{"Exact match", "Anime Title", "Anime Title", true},
		{"Case insensitive", "Anime Title", "anime title", true},
		{"With special chars removed", "Anime-Title!", "AnimeTitle", true},
		{"Containment within threshold", "Anime Title S1", "Anime Title", true},
		{"Reverse containment within threshold", "Anime Title", "Anime Title S1", true},
		{"Too different", "Completely Different", "Anime Title", false},
		{"Length diff too large", "Short", "This is a very long anime title that exceeds threshold", false},
		{"Empty strings", "", "", true},
		{"One empty", "Anime", "", false},
		{"Similar with numbers", "Anime 2024", "Anime2024", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSimilarDirectoryName(tt.name1, tt.name2)
			if got != tt.want {
				t.Errorf("isSimilarDirectoryName(%q, %q) = %v, want %v", tt.name1, tt.name2, got, tt.want)
			}
		})
	}
}

func TestAbs(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want int
	}{
		{"Positive", 5, 5},
		{"Negative", -5, 5},
		{"Zero", 0, 0},
		{"Large positive", 1000, 1000},
		{"Large negative", -1000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := abs(tt.n)
			if got != tt.want {
				t.Errorf("abs(%d) = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}


// 批量操作相关方法（mock实现）


// 批量操作相关方法（mock实现）
func (m *MockSubscriptionRepository) BatchUpdateEnabled(ids []uint, enabled bool) error {
	return nil
}

func (m *MockSubscriptionRepository) BatchDelete(ids []uint) error {
	return nil
}

func (m *MockSubscriptionRepository) BatchUpdateGroup(ids []uint, groupID *uint) error {
	return nil
}

// 分组管理相关方法（mock实现）
func (m *MockSubscriptionRepository) CreateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *MockSubscriptionRepository) UpdateGroup(group *model.SubscriptionGroup) error {
	return nil
}

func (m *MockSubscriptionRepository) DeleteGroup(id uint) error {
	return nil
}

func (m *MockSubscriptionRepository) GetGroupByID(id uint) (*model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) ListGroups() ([]model.SubscriptionGroup, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetDefaultGroup() (*model.SubscriptionGroup, error) {
	return nil, nil
}

// 统计相关方法（mock实现）
func (m *MockSubscriptionRepository) GetStatistics() (*repository.SubscriptionStatistics, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetWeeklyUpdates() (int64, error) {
	return 0, nil
}
