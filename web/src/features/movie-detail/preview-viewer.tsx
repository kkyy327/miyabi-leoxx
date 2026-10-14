import { XIcon } from 'lucide-react'
import { memo, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

import type { PreviewImage } from '@/api/discover'
import { MediaImage } from '@/components/media-image'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { cn } from 'cn'
import { createPreviewFrame } from './preview-frame'
import {
  DOUBLE_TAP_SCALE,
  FIT_SCALE,
  FIT_VIEW,
  PINCH_ZOOM_STEP,
  WHEEL_ZOOM_STEP,
  canPan,
  panBy,
  previewBase,
  stepIndex,
  wheelZoomFactor,
  zoomAt,
  type Size,
  type ZoomView
} from './preview-zoom'
import type {
  KeyboardEvent as ReactKeyboardEvent,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent
} from 'react'

/** Pointer travel, in pixels, that turns a click into a pan. */
const DRAG_THRESHOLD = 4

/** Horizontal pointer travel, in pixels, that turns a swipe into a page switch. */
const SWIPE_THRESHOLD = 60

/** A swipe must be mostly horizontal: vertical travel stays below this share of horizontal. */
const SWIPE_VERTICAL_RATIO = 0.6

/** A swipe must finish within this long, in milliseconds, to count as a flick. */
const SWIPE_MAX_DURATION_MS = 1000

/** After a swipe switches the page, ignore clicks for this long so the page switch is not read as "close". */
const SWIPE_CLICK_GUARD_MS = 500

const PreviewMediaImage = memo(MediaImage)

export function MoviePreviewViewer({
  images,
  index,
  open,
  onIndexChange,
  onClose
}: {
  images: PreviewImage[]
  index: number
  open: boolean
  onIndexChange: (index: number) => void
  onClose: () => void
}) {
  const stripRef = useRef<HTMLDivElement>(null)
  // Timestamp of the last swipe page-switch, so the click that follows it is not read as "close".
  const lastSwipeAtRef = useRef(0)
  // Sizes of the frames seen so far: previews of one movie almost always share their dimensions,
  // so seeding the next frame from the previous one avoids a resize flash on every switch.
  const [frameSize, setFrameSize] = useState<Size | null>(null)
  const total = images.length
  // The detail query can refetch a shorter list while the viewer is open.
  const activeIndex = total === 0 ? 0 : Math.min(index, total - 1)
  const current = images[activeIndex]

  const handleSwipe = useCallback(
    (direction: -1 | 1, timeStamp: number) => {
      lastSwipeAtRef.current = timeStamp
      onIndexChange(stepIndex(activeIndex, total, direction))
    },
    [activeIndex, onIndexChange, total]
  )

  // Keep the active thumbnail in view without scrollIntoView, which would move the page as well.
  useEffect(() => {
    const strip = stripRef.current
    const active = strip?.querySelector<HTMLElement>('[data-active="true"]')
    if (!strip || !active) return
    const stripRect = strip.getBoundingClientRect()
    const activeRect = active.getBoundingClientRect()
    const centerOffset = stripRect.width / 2 - activeRect.width / 2
    strip.scrollTo({
      left: strip.scrollLeft + (activeRect.left - stripRect.left) - centerOffset,
      behavior: 'smooth'
    })
  }, [activeIndex, open])

  const handleKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    onIndexChange(stepIndex(activeIndex, total, event.key === 'ArrowLeft' ? -1 : 1))
  }

  if (!current) return null

  return (
    <Dialog
      open={open}
      onOpenChange={next => {
        if (!next) onClose()
      }}
    >
      <DialogContent
        showCloseButton={false}
        onKeyDown={handleKeyDown}
        className="dark inset-0 flex max-w-none translate-x-0 translate-y-0 flex-col gap-0 rounded-none bg-transparent p-0 text-foreground ring-0 sm:max-w-none"
      >
        <div className="flex shrink-0 justify-end px-4 pt-4">
          <Button variant="ghost" size="icon" onClick={onClose}>
            <XIcon />
          </Button>
        </div>

        {/* Remounting on the index gives every image its own zoom, pan and measured size. */}
        <PreviewStage
          key={activeIndex}
          image={current}
          initialNatural={frameSize}
          onNaturalSize={setFrameSize}
          onSwipe={handleSwipe}
          swipeGuardRef={lastSwipeAtRef}
          onClose={onClose}
        />

        <p className="shrink-0 px-4 text-center text-xs font-medium text-foreground/70 tabular-nums">
          {activeIndex + 1} / {total}
        </p>

        <div
          ref={stripRef}
          className="flex shrink-0 scrollbar-none overflow-x-auto px-4 pt-4 pb-6 [&::-webkit-scrollbar]:hidden"
        >
          {/* Centered while the thumbnails fit, still scrollable once they do not. */}
          <div className="mx-auto flex items-center gap-2">
            {images.map((image, itemIndex) => (
              <button
                key={`${image.original}:${itemIndex}`}
                type="button"
                data-active={itemIndex === activeIndex ? 'true' : undefined}
                onClick={() => onIndexChange(itemIndex)}
                className={cn(
                  'group shrink-0 cursor-pointer rounded-xl p-1 transition-colors',
                  itemIndex === activeIndex ? 'bg-foreground/15' : 'hover:bg-foreground/10'
                )}
              >
                <span
                  className={cn(
                    'block aspect-video w-16 overflow-hidden rounded-lg transition-opacity sm:w-20',
                    itemIndex === activeIndex ? 'opacity-100' : 'opacity-50 group-hover:opacity-100'
                  )}
                >
                  <MediaImage source={image.thumbnail || image.original} className="object-cover" />
                </span>
              </button>
            ))}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function PreviewStage({
  image,
  initialNatural,
  onNaturalSize,
  onSwipe,
  swipeGuardRef,
  onClose
}: {
  image: PreviewImage
  /** Size of the image last shown, so switching frames does not resize the box from scratch. */
  initialNatural: Size | null
  onNaturalSize: (size: Size) => void
  /** Swipe left = next image, swipe right = previous image. Only fires at fit scale. */
  onSwipe: (direction: -1 | 1, timeStamp: number) => void
  /** Timestamp of the last swipe page-switch, used to ignore the click that follows it. */
  swipeGuardRef: { current: number }
  onClose: () => void
}) {
  const [view, setView] = useState<ZoomView>(FIT_VIEW)
  const [viewFrame] = useState(() => createPreviewFrame(setView))
  const [stageSize, setStageSize] = useState<Size>({ width: 0, height: 0 })
  const [naturalSize, setNaturalSize] = useState<Size | null>(initialNatural)
  const stageRef = useRef<HTMLDivElement>(null)
  const frameRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ pointerId: number; x: number; y: number } | null>(null)
  const draggedRef = useRef(false)
  const swipeRef = useRef<{ x: number; y: number; time: number } | null>(null)

  const { width: stageWidth, height: stageHeight } = stageSize
  const naturalWidth = naturalSize?.width ?? 0
  const naturalHeight = naturalSize?.height ?? 0
  const base = previewBase(naturalSize, stageSize)
  const pannable = canPan(view, base, stageSize)

  useEffect(() => () => viewFrame.cancel(), [viewFrame])

  const handleImageLoad = useCallback(
    (element: HTMLImageElement) => {
      const size = { width: element.naturalWidth, height: element.naturalHeight }
      setNaturalSize(size)
      onNaturalSize(size)
    },
    [onNaturalSize]
  )

  // Measure the padded box, not the frame around it, so the image keeps its margins.
  useLayoutEffect(() => {
    const element = stageRef.current
    if (!element) return
    const measure = () => setStageSize({ width: element.clientWidth, height: element.clientHeight })
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  // Wheel zoom has to preventDefault, which a passive React listener is not allowed to do.
  // It covers the padded frame as well, so the whole area below the header zooms.
  useEffect(() => {
    const element = frameRef.current
    const stageElement = stageRef.current
    if (!element || !stageElement) return
    const natural = naturalWidth > 0 ? { width: naturalWidth, height: naturalHeight } : null
    const stage = { width: stageWidth, height: stageHeight }
    const baseSize = previewBase(natural, stage)
    const handleWheel = (event: WheelEvent) => {
      event.preventDefault()
      const rect = stageElement.getBoundingClientRect()
      const anchor = {
        x: event.clientX - (rect.left + rect.width / 2),
        y: event.clientY - (rect.top + rect.height / 2)
      }
      const step = event.ctrlKey ? PINCH_ZOOM_STEP : WHEEL_ZOOM_STEP
      const factor = wheelZoomFactor(event.deltaY, event.deltaMode, step)
      viewFrame.update(current => zoomAt(current, anchor, factor, baseSize, stage))
    }
    element.addEventListener('wheel', handleWheel, { passive: false })
    return () => element.removeEventListener('wheel', handleWheel)
  }, [naturalWidth, naturalHeight, stageWidth, stageHeight, viewFrame])

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    draggedRef.current = false
    // Track every gesture start for swipe detection, even when the image is not pannable.
    swipeRef.current = { x: event.clientX, y: event.clientY, time: event.timeStamp }
    if (!canPan(viewFrame.current(), base, stageSize) || event.button !== 0) return
    dragRef.current = { pointerId: event.pointerId, x: event.clientX, y: event.clientY }
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== event.pointerId) return
    const dx = event.clientX - drag.x
    const dy = event.clientY - drag.y
    if (!draggedRef.current && Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD)
      return
    draggedRef.current = true
    drag.x = event.clientX
    drag.y = event.clientY
    viewFrame.update(current => panBy(current, dx, dy, base, stageSize))
  }

  const handlePointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (drag && drag.pointerId === event.pointerId) {
      dragRef.current = null
      if (event.currentTarget.hasPointerCapture(event.pointerId)) {
        event.currentTarget.releasePointerCapture(event.pointerId)
      }
    }
    // Horizontal swipe at fit scale switches the page; a zoomed image pans instead.
    const swipe = swipeRef.current
    swipeRef.current = null
    if (
      swipe &&
      !draggedRef.current &&
      viewFrame.current().scale === FIT_SCALE &&
      event.timeStamp - swipe.time <= SWIPE_MAX_DURATION_MS
    ) {
      const dx = event.clientX - swipe.x
      const dy = event.clientY - swipe.y
      if (Math.abs(dx) > SWIPE_THRESHOLD && Math.abs(dy) < Math.abs(dx) * SWIPE_VERTICAL_RATIO) {
        onSwipe(dx < 0 ? 1 : -1, event.timeStamp)
      }
    }
  }

  const handleClick = (event: ReactMouseEvent<HTMLDivElement>) => {
    // A pan ends with a click, which must not be read as "close".
    if (draggedRef.current) {
      draggedRef.current = false
      return
    }
    // A swipe page-switch ends with a click, which must not be read as "close" either.
    if (event.timeStamp - swipeGuardRef.current < SWIPE_CLICK_GUARD_MS) return
    if ((event.target as HTMLElement).closest('[data-preview-image]')) return
    onClose()
  }

  const handleDoubleClick = (event: ReactMouseEvent<HTMLDivElement>) => {
    const element = stageRef.current
    if (!element) return
    const rect = element.getBoundingClientRect()
    const anchor = {
      x: event.clientX - (rect.left + rect.width / 2),
      y: event.clientY - (rect.top + rect.height / 2)
    }
    viewFrame.update(current =>
      current.scale === FIT_SCALE
        ? zoomAt(current, anchor, DOUBLE_TAP_SCALE, base, stageSize)
        : FIT_VIEW
    )
  }

  return (
    <div ref={frameRef} className="min-h-0 flex-1 p-3 sm:p-6" onClick={handleClick}>
      <div
        ref={stageRef}
        className={cn(
          'relative flex size-full touch-none items-center justify-center overflow-hidden select-none',
          pannable && 'cursor-grab active:cursor-grabbing'
        )}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerUp}
        onDoubleClick={handleDoubleClick}
        onDragStart={event => event.preventDefault()}
      >
        <div
          data-preview-image
          className="overflow-hidden rounded-2xl bg-muted will-change-transform"
          style={{
            width: base.width,
            height: base.height,
            transform: `translate3d(${view.x}px, ${view.y}px, 0) scale(${view.scale})`
          }}
        >
          <PreviewMediaImage
            source={image.original || image.thumbnail}
            loading="eager"
            onImageLoad={handleImageLoad}
            className="object-contain"
          />
        </div>
      </div>
    </div>
  )
}
