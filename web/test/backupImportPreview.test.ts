import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import {
  isPreviewedImportCurrent,
  type BackupImportInputSnapshot,
  type BackupImportStrategy,
  type PreviewedImport
} from '../src/utils/backupImportPreview.ts'

type TestPlan = {
  summary: {
    create: number
    overwrite: number
    merge: number
  }
}

type Deferred<T> = {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (error: unknown) => void
}

const createDeferred = <T>(): Deferred<T> => {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })

  return { promise, resolve, reject }
}

class BackupImportHarness {
  sourceFormat = 'auto'
  strategy: BackupImportStrategy = 'skip'
  fileName = ''
  importData: unknown | null = null
  plan: TestPlan | null = null
  previewedImport: PreviewedImport<TestPlan> | null = null
  importFileGeneration = 0
  messages: string[] = []

  setSourceFormat(sourceFormat: string) {
    this.sourceFormat = sourceFormat
    this.invalidateImportPreview()
  }

  setStrategy(strategy: BackupImportStrategy) {
    this.strategy = strategy
    this.invalidateImportPreview()
  }

  async selectFile(file: { name: string; text: () => Promise<string> } | null) {
    const generation = ++this.importFileGeneration

    if (!file) {
      this.clearImportState()
      return
    }

    this.fileName = file.name
    this.importData = null
    this.invalidateImportPreview()

    try {
      const text = await file.text()
      const parsed = JSON.parse(text)
      if (generation !== this.importFileGeneration) {
        return
      }
      this.importData = parsed
    } catch {
      if (generation !== this.importFileGeneration) {
        return
      }
      this.clearImportState()
      this.messages.push('invalid-json')
    }
  }

  async previewImport(preview: (snapshot: BackupImportInputSnapshot) => Promise<TestPlan>) {
    if (!this.importData) {
      this.messages.push('missing-file')
      return
    }

    const previewSnapshot = this.currentImportInputSnapshot()
    const nextPlan = await preview(previewSnapshot)

    if (!isPreviewedImportCurrent(previewSnapshot, this.currentImportInputSnapshot())) {
      this.invalidateImportPreview()
      this.messages.push('stale-preview')
      return
    }

    this.plan = nextPlan
    this.previewedImport = {
      ...previewSnapshot,
      plan: nextPlan
    }
  }

  applyImport(importBackup: (snapshot: BackupImportInputSnapshot) => Promise<TestPlan>) {
    const preview = this.previewedImport
    if (!preview) {
      return null
    }

    if (!isPreviewedImportCurrent(preview, this.currentImportInputSnapshot())) {
      this.invalidateImportPreview()
      this.messages.push('stale-apply')
      return null
    }

    return async () => {
      if (!isPreviewedImportCurrent(preview, this.currentImportInputSnapshot())) {
        this.invalidateImportPreview()
        this.messages.push('stale-confirm')
        return
      }

      const importedPlan = await importBackup({
        data: preview.data,
        sourceFormat: preview.sourceFormat,
        strategy: preview.strategy
      })

      if (!isPreviewedImportCurrent(preview, this.currentImportInputSnapshot())) {
        this.invalidateImportPreview()
        this.messages.push('imported-after-drift')
        return
      }

      this.plan = importedPlan
      this.previewedImport = {
        ...preview,
        plan: importedPlan
      }
    }
  }

  private invalidateImportPreview() {
    this.plan = null
    this.previewedImport = null
  }

  private clearImportState() {
    this.importFileGeneration += 1
    this.fileName = ''
    this.importData = null
    this.invalidateImportPreview()
  }

  private currentImportInputSnapshot(): BackupImportInputSnapshot {
    return {
      data: this.importData,
      sourceFormat: this.sourceFormat,
      strategy: this.strategy
    }
  }
}

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

  it('invalidates preview state when source format or conflict strategy changes', () => {
    const data = { subscriptions: [] }
    const plan = { summary: { create: 1, overwrite: 0, merge: 0 } }
    const harness = new BackupImportHarness()
    harness.importData = data
    harness.sourceFormat = 'auto-rss'
    harness.strategy = 'merge'
    harness.plan = plan
    harness.previewedImport = {
      data,
      sourceFormat: 'auto-rss',
      strategy: 'merge',
      plan
    }

    harness.setSourceFormat('auto-bangumi')

    assert.equal(harness.plan, null)
    assert.equal(harness.previewedImport, null)

    harness.importData = data
    harness.plan = plan
    harness.previewedImport = {
      data,
      sourceFormat: 'auto-bangumi',
      strategy: 'merge',
      plan
    }

    harness.setStrategy('overwrite')

    assert.equal(harness.plan, null)
    assert.equal(harness.previewedImport, null)
  })

  it('invalidates preview state before a replacement selected file finishes parsing', async () => {
    const oldData = { subscriptions: [{ name: 'old' }] }
    const newFileRead = createDeferred<string>()
    const harness = new BackupImportHarness()
    harness.importData = oldData
    harness.plan = { summary: { create: 1, overwrite: 0, merge: 0 } }
    harness.previewedImport = {
      data: oldData,
      sourceFormat: 'auto',
      strategy: 'skip',
      plan: harness.plan
    }

    const pendingSelection = harness.selectFile({
      name: 'replacement.json',
      text: () => newFileRead.promise
    })

    assert.equal(harness.fileName, 'replacement.json')
    assert.equal(harness.importData, null)
    assert.equal(harness.plan, null)
    assert.equal(harness.previewedImport, null)

    const replacementData = { subscriptions: [{ name: 'replacement' }] }
    newFileRead.resolve(JSON.stringify(replacementData))
    await pendingSelection

    assert.deepEqual(harness.importData, replacementData)
  })

  it('ignores stale async file-read generations when a newer selection wins', async () => {
    const firstRead = createDeferred<string>()
    const secondRead = createDeferred<string>()
    const harness = new BackupImportHarness()

    const firstSelection = harness.selectFile({
      name: 'first.json',
      text: () => firstRead.promise
    })
    const secondSelection = harness.selectFile({
      name: 'second.json',
      text: () => secondRead.promise
    })

    firstRead.resolve(JSON.stringify({ subscriptions: [{ name: 'first' }] }))
    await firstSelection

    assert.equal(harness.fileName, 'second.json')
    assert.equal(harness.importData, null)

    const secondData = { subscriptions: [{ name: 'second' }] }
    secondRead.resolve(JSON.stringify(secondData))
    await secondSelection

    assert.deepEqual(harness.importData, secondData)
  })

  it('rejects preview responses when parsed data generation changes after request start', async () => {
    const firstData = { subscriptions: [{ name: 'first' }] }
    const secondData = { subscriptions: [{ name: 'second' }] }
    const previewResponse = createDeferred<TestPlan>()
    const harness = new BackupImportHarness()
    harness.importData = firstData
    harness.sourceFormat = 'auto-rss'
    harness.strategy = 'merge'

    const previewing = harness.previewImport(async () => previewResponse.promise)
    harness.importData = secondData
    previewResponse.resolve({ summary: { create: 1, overwrite: 0, merge: 0 } })
    await previewing

    assert.equal(harness.plan, null)
    assert.equal(harness.previewedImport, null)
    assert.deepEqual(harness.messages, ['stale-preview'])
  })

  it('keeps confirmation and import requests bound to the previewed tuple', async () => {
    const previewData = { subscriptions: [{ name: 'previewed' }] }
    const importedPlan = { summary: { create: 0, overwrite: 1, merge: 0 } }
    const importRequests: BackupImportInputSnapshot[] = []
    const harness = new BackupImportHarness()
    harness.importData = previewData
    harness.sourceFormat = 'auto-rss'
    harness.strategy = 'merge'
    harness.previewedImport = {
      data: previewData,
      sourceFormat: 'auto-rss',
      strategy: 'merge',
      plan: { summary: { create: 1, overwrite: 0, merge: 0 } }
    }

    const confirmImport = harness.applyImport(async (snapshot) => {
      importRequests.push(snapshot)
      return importedPlan
    })

    assert.notEqual(confirmImport, null)
    await confirmImport?.()

    assert.deepEqual(importRequests, [
      {
        data: previewData,
        sourceFormat: 'auto-rss',
        strategy: 'merge'
      }
    ])
    assert.equal(harness.plan, importedPlan)
    assert.deepEqual(harness.previewedImport, {
      data: previewData,
      sourceFormat: 'auto-rss',
      strategy: 'merge',
      plan: importedPlan
    })
  })

  it('rejects confirmation when source, strategy, or selected file drift after the dialog opens', async () => {
    const previewData = { subscriptions: [{ name: 'previewed' }] }
    const harness = new BackupImportHarness()
    harness.importData = previewData
    harness.sourceFormat = 'auto-rss'
    harness.strategy = 'merge'
    harness.previewedImport = {
      data: previewData,
      sourceFormat: 'auto-rss',
      strategy: 'merge',
      plan: { summary: { create: 1, overwrite: 0, merge: 0 } }
    }

    const confirmImport = harness.applyImport(async () => {
      throw new Error('stale confirmation must not import')
    })

    assert.notEqual(confirmImport, null)
    harness.importData = { subscriptions: [{ name: 'replacement' }] }
    await confirmImport?.()

    assert.equal(harness.plan, null)
    assert.equal(harness.previewedImport, null)
    assert.deepEqual(harness.messages, ['stale-confirm'])
  })
})
