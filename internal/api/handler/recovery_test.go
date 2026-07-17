package handler

import (
	"testing"

	"github.com/WormW/auto-rss/internal/service/recovery"
)

func TestSummarizeRecoveryScanBoundsSubscriptionSamples(t *testing.T) {
	overflow := RecoveryPreviewSampleLimit + 3
	result := &recovery.ScanResult{
		ScannedFiles: 100,
		MatchedFiles: 80,
		OrphanFiles:  numberedStrings("orphan", overflow),
		Subscriptions: []recovery.SubscriptionScanResult{
			{
				SubscriptionID:    42,
				Name:              "Large Library",
				CurrentEpisodeOld: 1,
				CurrentEpisodeNew: overflow,
				LatestEpisodeOld:  1,
				LatestEpisodeNew:  overflow,
				EpisodesOnDisk:    numberedInts(overflow),
				MatchedEpisodes:   numberedEpisodeFiles(overflow),
				DownloadsToUpdate: numberedUints(overflow),
				DownloadsToCreate: numberedInts(overflow),
				DownloadsMissing:  numberedUints(overflow),
			},
		},
	}

	out := summarizeRecoveryScan(result, true)

	if !out.DryRun || !out.PreviewOnly || out.Applied {
		t.Fatalf("preview flags = dry_run %v preview_only %v applied %v, want true true false", out.DryRun, out.PreviewOnly, out.Applied)
	}
	if out.OrphanFileCount != overflow || len(out.OrphanFileSamples) != RecoveryPreviewSampleLimit || out.OrphanFileOmittedCount != 3 {
		t.Fatalf("orphan bounds = count %d samples %d omitted %d, want %d %d 3", out.OrphanFileCount, len(out.OrphanFileSamples), out.OrphanFileOmittedCount, overflow, RecoveryPreviewSampleLimit)
	}
	if out.DownloadsToUpdateCount != overflow || out.DownloadsToCreateCount != overflow || out.DownloadsMissingCount != overflow {
		t.Fatalf("total counts = update %d create %d missing %d, want %d", out.DownloadsToUpdateCount, out.DownloadsToCreateCount, out.DownloadsMissingCount, overflow)
	}
	if out.SubscriptionCount != 1 || len(out.Subscriptions) != 1 {
		t.Fatalf("subscription count=%d len=%d, want 1", out.SubscriptionCount, len(out.Subscriptions))
	}

	sub := out.Subscriptions[0]
	if sub.EpisodesOnDiskCount != overflow || len(sub.EpisodeSamples) != RecoveryPreviewSampleLimit || sub.EpisodeOmittedCount != 3 {
		t.Fatalf("episode bounds = count %d samples %d omitted %d, want %d %d 3", sub.EpisodesOnDiskCount, len(sub.EpisodeSamples), sub.EpisodeOmittedCount, overflow, RecoveryPreviewSampleLimit)
	}
	if sub.MatchedEpisodeCount != overflow || len(sub.MatchedEpisodeSamples) != RecoveryPreviewSampleLimit || sub.MatchedEpisodeOmittedCount != 3 {
		t.Fatalf("matched bounds = count %d samples %d omitted %d, want %d %d 3", sub.MatchedEpisodeCount, len(sub.MatchedEpisodeSamples), sub.MatchedEpisodeOmittedCount, overflow, RecoveryPreviewSampleLimit)
	}
	if sub.DownloadsToUpdateCount != overflow || len(sub.DownloadsToUpdateIDs) != RecoveryPreviewSampleLimit {
		t.Fatalf("update bounds = count %d samples %d, want %d %d", sub.DownloadsToUpdateCount, len(sub.DownloadsToUpdateIDs), overflow, RecoveryPreviewSampleLimit)
	}
	if sub.DownloadsToCreateCount != overflow || len(sub.DownloadsToCreate) != RecoveryPreviewSampleLimit {
		t.Fatalf("create bounds = count %d samples %d, want %d %d", sub.DownloadsToCreateCount, len(sub.DownloadsToCreate), overflow, RecoveryPreviewSampleLimit)
	}
	if sub.DownloadsMissingCount != overflow || len(sub.DownloadsMissingIDs) != RecoveryPreviewSampleLimit {
		t.Fatalf("missing bounds = count %d samples %d, want %d %d", sub.DownloadsMissingCount, len(sub.DownloadsMissingIDs), overflow, RecoveryPreviewSampleLimit)
	}
}

func numberedStrings(prefix string, count int) []string {
	values := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		values = append(values, prefix)
	}
	return values
}

func numberedInts(count int) []int {
	values := make([]int, 0, count)
	for i := 1; i <= count; i++ {
		values = append(values, i)
	}
	return values
}

func numberedUints(count int) []uint {
	values := make([]uint, 0, count)
	for i := 1; i <= count; i++ {
		values = append(values, uint(i))
	}
	return values
}

func numberedEpisodeFiles(count int) []recovery.EpisodeFile {
	files := make([]recovery.EpisodeFile, 0, count)
	for i := 1; i <= count; i++ {
		files = append(files, recovery.EpisodeFile{Path: "episode.mkv", Episode: i, Season: 1})
	}
	return files
}
