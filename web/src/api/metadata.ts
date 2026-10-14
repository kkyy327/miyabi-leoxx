import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { apiGet, apiPut } from './client'

export type ScrapingSource = { id: string; enabled: boolean }
const sourcesKey = ['settings', 'scraping'] as const

export const sourceNames: Record<string, string> = {
  fanza: 'FANZA',
  fc2: 'FC2 官方',
  heyzo: 'HEYZO 官方',
  pacopacomama: 'Pacopacomama 官方'
}

export function useScrapingSources() {
  return useQuery({
    queryKey: sourcesKey,
    queryFn: ({ signal }) => apiGet<ScrapingSource[]>('/api/settings/scraping', undefined, signal)
  })
}

export function useUpdateScrapingSources() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (sources: ScrapingSource[]) =>
      apiPut<ScrapingSource[]>('/api/settings/scraping', sources),
    onSuccess: sources => {
      client.setQueryData(sourcesKey, sources)
    }
  })
}

export type MetaTubePostersSetting = { enabled: boolean }
const metatubePostersKey = ['settings', 'metatube-posters'] as const

export function useMetaTubePosters() {
  return useQuery({
    queryKey: metatubePostersKey,
    queryFn: ({ signal }) =>
      apiGet<MetaTubePostersSetting>('/api/settings/metatube-posters', undefined, signal)
  })
}

export function useUpdateMetaTubePosters() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (setting: MetaTubePostersSetting) =>
      apiPut<MetaTubePostersSetting>('/api/settings/metatube-posters', setting),
    onSuccess: setting => {
      client.setQueryData(metatubePostersKey, setting)
    }
  })
}
