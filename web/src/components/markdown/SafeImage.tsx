import { useState } from 'react'

import { cn } from '@/utils'

import { isAllowedImageSource } from './markdown'

export interface SafeImageProps {
  src?: string
  alt?: string
  className?: string
}

function unavailableLabel(alt: string | undefined): string {
  return alt ? `图片不可用：${alt}` : '图片不可用'
}

export default function SafeImage({ src, alt, className }: SafeImageProps) {
  const [failedSource, setFailedSource] = useState<string>()
  const unavailable = src === undefined || !isAllowedImageSource(src) || failedSource === src

  if (unavailable) {
    return (
      <span
        data-testid="safe-image-wrapper"
        className="my-3 inline-flex min-h-24 w-full max-w-md items-center justify-center overflow-hidden rounded-md border border-gray-200 bg-gray-50 px-4 py-3 text-sm text-gray-600"
      >
        <span data-image-unavailable>{unavailableLabel(alt)}</span>
      </span>
    )
  }

  return (
    <span data-testid="safe-image-wrapper" className="my-3 block max-w-full overflow-x-auto">
      <img
        src={src}
        alt={alt ?? ''}
        loading="lazy"
        decoding="async"
        referrerPolicy="no-referrer"
        width={640}
        height={360}
        onError={() => setFailedSource(src)}
        className={cn('block h-auto max-w-full rounded-md object-contain', className)}
      />
    </span>
  )
}
