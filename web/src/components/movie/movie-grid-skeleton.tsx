import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useSettingsStore } from '@/stores/settings'

import { MovieGridLayout } from './movie-grid-layout'

export function MovieGridSkeleton({
  count = 8,
  compact = false
}: {
  count?: number
  compact?: boolean
}) {
  return (
    <MovieGridLayout>
      {Array.from({ length: count }, (_, index) => (
        <MovieCardSkeleton key={index} compact={compact} />
      ))}
    </MovieGridLayout>
  )
}

export function MovieCardSkeleton({ compact = false }: { compact?: boolean }) {
  const coverStyle = useSettingsStore(state => state.coverStyle)
  return (
    <Card size="sm" className="h-full gap-0 overflow-hidden py-0">
      <Skeleton
        className={`${coverStyle === 'poster' ? 'aspect-2/3' : 'aspect-3/2'} w-full rounded-none`}
      />
      <CardContent className="space-y-2 p-3">
        <Skeleton className="h-5 w-full" />
        {compact ? null : <Skeleton className="h-4 w-3/4" />}
        <Skeleton className="h-5 w-1/2" />
      </CardContent>
    </Card>
  )
}
