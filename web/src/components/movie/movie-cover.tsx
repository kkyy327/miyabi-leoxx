import { MediaImage } from '@/components/media-image'

export function MovieCover({
  source,
  fallback,
  loading = 'lazy',
  onReady
}: {
  source: string
  fallback?: string
  loading?: 'eager' | 'lazy'
  onReady?: () => void
}) {
  return (
    <MediaImage
      source={source}
      fallback={fallback}
      loading={loading}
      onReady={onReady}
      blurredBackground
      className="object-contain"
    />
  )
}
