import * as DialogPrimitive from '@radix-ui/react-dialog'
import { useRef, type ComponentProps } from 'react'
import { cn } from '@/utils'

export const Dialog = DialogPrimitive.Root
export const DialogTitle = DialogPrimitive.Title
export const DialogDescription = DialogPrimitive.Description

// One primitive for both public ConfirmDialog and Runtime Recovery.
// Callers may be conditionally mounted without a Radix Trigger; retain their opener.
export function DialogContent({ className, children, onOpenAutoFocus, onCloseAutoFocus, ...props }: ComponentProps<typeof DialogPrimitive.Content>) {
  const opener = useRef<HTMLElement | null>(null)
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/40" />
      <DialogPrimitive.Content
        {...props}
        className={cn('fixed left-1/2 top-1/2 z-50 max-h-[calc(100vh-2rem)] w-[calc(100%-2rem)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl bg-white p-6 shadow-xl', className)}
        onOpenAutoFocus={event => {
          opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
          onOpenAutoFocus?.(event)
        }}
        onCloseAutoFocus={event => {
          onCloseAutoFocus?.(event)
          if (!event.defaultPrevented && opener.current?.isConnected) {
            event.preventDefault()
            opener.current.focus()
          }
        }}
      >{children}</DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}
