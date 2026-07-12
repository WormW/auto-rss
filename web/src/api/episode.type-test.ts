import { episodeApi } from './episode'

void episodeApi.updateStatus(1, [1], 'ignored')

// @ts-expect-error backend rejects derived states
void episodeApi.updateStatus(1, [1], 'downloaded')

// @ts-expect-error backend rejects derived states
void episodeApi.updateStatus(1, [1], 'downloading')
