package repository

import (
	"testing"

	"github.com/WormW/auto-rss/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDeleteSubscriptionDeletesEpisodeLedger(t *testing.T) {
	db, episodeRepo := setupEpisodeRepository(t)
	sub := model.Subscription{Name: "delete me"}
	require.NoError(t, db.Create(&sub).Error)
	episode, err := episodeRepo.ObserveEpisode(sub.ID, 1)
	require.NoError(t, err)
	_, _, err = episodeRepo.UpsertCandidate(episode.ID, &model.EpisodeResourceCandidate{
		ResourceKey: "hash:a",
		Status:      model.CandidateStatusPending,
	})
	require.NoError(t, err)

	require.NoError(t, NewSubscriptionRepository(db).Delete(sub.ID))

	assertTableCount(t, db, &model.Subscription{}, 0)
	assertTableCount(t, db, &model.SubscriptionEpisode{}, 0)
	assertTableCount(t, db, &model.EpisodeResourceCandidate{}, 0)
}

func TestBatchDeleteSubscriptionsDeletesEpisodeLedgers(t *testing.T) {
	db, episodeRepo := setupEpisodeRepository(t)
	subs := []model.Subscription{{Name: "one"}, {Name: "two"}, {Name: "keep"}}
	require.NoError(t, db.Create(&subs).Error)
	for _, sub := range subs {
		episode, err := episodeRepo.ObserveEpisode(sub.ID, 1)
		require.NoError(t, err)
		_, _, err = episodeRepo.UpsertCandidate(episode.ID, &model.EpisodeResourceCandidate{
			ResourceKey: "hash:a",
			Status:      model.CandidateStatusPending,
		})
		require.NoError(t, err)
	}

	require.NoError(t, NewSubscriptionRepository(db).BatchDelete([]uint{subs[0].ID, subs[1].ID}))

	assertTableCount(t, db, &model.Subscription{}, 1)
	assertTableCount(t, db, &model.SubscriptionEpisode{}, 1)
	assertTableCount(t, db, &model.EpisodeResourceCandidate{}, 1)
}

func TestDeleteSubscriptionSupportsLegacySchemaWithoutEpisodeTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}))
	sub := model.Subscription{Name: "legacy"}
	require.NoError(t, db.Create(&sub).Error)

	require.NoError(t, NewSubscriptionRepository(db).Delete(sub.ID))
	assertTableCount(t, db, &model.Subscription{}, 0)
}

func TestBatchDeleteSubscriptionsSupportsLegacySchemaWithoutEpisodeTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Subscription{}))
	require.NoError(t, db.Create(&[]model.Subscription{{Name: "one"}, {Name: "two"}}).Error)

	require.NoError(t, NewSubscriptionRepository(db).BatchDelete([]uint{1, 2}))
	assertTableCount(t, db, &model.Subscription{}, 0)
}

func assertTableCount(t *testing.T, db *gorm.DB, value any, want int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(value).Count(&count).Error)
	assert.Equal(t, want, count)
}
