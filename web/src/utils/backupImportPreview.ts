export type BackupImportStrategy = 'skip' | 'overwrite' | 'merge'

export type BackupImportInputSnapshot = {
  data: unknown
  sourceFormat: string
  strategy: BackupImportStrategy
}

export type PreviewedImport<TPlan> = BackupImportInputSnapshot & {
  plan: TPlan
}

export const isPreviewedImportCurrent = (
  preview: BackupImportInputSnapshot | null | undefined,
  current: BackupImportInputSnapshot
) => {
  return Boolean(
    preview &&
      current.data === preview.data &&
      current.sourceFormat === preview.sourceFormat &&
      current.strategy === preview.strategy
  )
}
