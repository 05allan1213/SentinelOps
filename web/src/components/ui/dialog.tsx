import * as DialogPrimitive from '@radix-ui/react-dialog'
import { useRef, type ComponentProps } from 'react'
import { cn } from '@/utils'

export function Dialog(props: ComponentProps<typeof DialogPrimitive.Root>) { return <DialogPrimitive.Root {...props} /> }
export function DialogTitle({ className, ...props }: ComponentProps<typeof DialogPrimitive.Title>) {
  return <DialogPrimitive.Title {...props} className={cn('text-lg font-semibold leading-6 text-gray-900', className)} />
}
export function DialogDescription({ className, ...props }: ComponentProps<typeof DialogPrimitive.Description>) {
  return <DialogPrimitive.Description {...props} className={cn('text-sm leading-[22px] text-gray-600', className)} />
}
export function DialogTrigger(props: ComponentProps<typeof DialogPrimitive.Trigger>) { return <DialogPrimitive.Trigger {...props} /> }
export function DialogClose(props: ComponentProps<typeof DialogPrimitive.Close>) { return <DialogPrimitive.Close {...props} /> }

export function DialogHeader({ className, ...props }: ComponentProps<'div'>) {
  return <div {...props} className={cn('min-w-0 shrink-0 space-y-2 pb-4 [overflow-wrap:anywhere]', className)} />
}
export function DialogBody({ className, ...props }: ComponentProps<'div'>) {
  return <div {...props} className={cn('min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain py-1 text-sm leading-[22px] [overflow-wrap:anywhere]', className)} />
}
export function DialogFooter({ className, ...props }: ComponentProps<'div'>) {
  return <div {...props} className={cn('flex shrink-0 flex-wrap items-center justify-end gap-2 pt-4', className)} />
}

// One primitive for both public ConfirmDialog and Runtime Recovery.
// Callers may be conditionally mounted without a Radix Trigger; retain their opener.
export function DialogContent({ className, children, onOpenAutoFocus, onCloseAutoFocus, ...props }: ComponentProps<typeof DialogPrimitive.Content>) {
  const opener = useRef<HTMLElement | null>(null)
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-black/40" />
      <DialogPrimitive.Content
        {...props}
        className={cn('fixed left-1/2 top-1/2 z-50 flex max-h-[calc(100vh-2rem)] w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 flex-col overflow-y-auto overscroll-contain rounded-xl bg-white p-6 shadow-xl', className)}
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
