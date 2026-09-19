package database

import (
	"path/filepath"
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/WormW/auto-rss/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestStartupMigrationSupportsTagManagement(t *testing.T) {
	db, err := Init(filepath.Join(t.TempDir(), "service.db"))
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, Migrate(db))
	require.NoError(t, RunMigrations(db))

	repo := repository.NewSubscriptionRepository(db)
	tag := &model.SubscriptionTag{Name: "watching"}
	require.NoError(t, repo.CreateTag(tag), "startup must create the tables used by tag management")
	sub := &model.Subscription{Name: "Test", RssURL: "https://example.test/feed"}
	require.NoError(t, repo.Create(sub))
	require.NoError(t, repo.AddTagsToSubscription(sub.ID, []uint{tag.ID}))

	// Repeated startup must preserve tags and their associations.
	require.NoError(t, Migrate(db))
	require.NoError(t, RunMigrations(db))
	tags, err := repo.GetSubscriptionTags(sub.ID)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, tag.ID, tags[0].ID)
}
