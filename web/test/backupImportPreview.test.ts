import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import { isPreviewedImportCurrent } from '../src/utils/backupImportPreview.ts'

describe('isPreviewedImportCurrent', () => {
  it('accepts the exact previewed source, strategy, and file data tuple', () => {
    const data = { subscriptions: [] }

    assert.equal(
      isPreviewedImportCurrent(
        { data, sourceFormat: 'auto-rss', strategy: 'merge' },
        { data, sourceFormat: 'auto-rss', strategy: 'merge' }
      ),
      true
    )
  })

  it('rejects missing or stale preview snapshots', () => {
    const data = { subscriptions: [] }

    assert.equal(
      isPreviewedImportCurrent(null, { data, sourceFormat: 'auto-rss', strategy: 'merge' }),
      false
    )
    assert.equal(
      isPreviewedImportCurrent(
        { data, sourceFormat: 'auto-rss', strategy: 'merge' },
        { data, sourceFormat: 'auto-bangumi', strategy: 'merge' }
      ),
      false
    )
    assert.equal(
      isPreviewedImportCurrent(
        { data, sourceFormat: 'auto-rss', strategy: 'merge' },
        { data, sourceFormat: 'auto-rss', strategy: 'overwrite' }
      ),
      false
    )
    assert.equal(
      isPreviewedImportCurrent(
        { data, sourceFormat: 'auto-rss', strategy: 'merge' },
        { data: { subscriptions: [] }, sourceFormat: 'auto-rss', strategy: 'merge' }
      ),
      false
    )
  })
})
