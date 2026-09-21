import { useEffect, useRef, useState } from 'react'
import { ArrowsIn, ArrowsOut, X } from '@phosphor-icons/react'
import { Badge, Button } from './UI'

export default function WorkPreviewDialog({ preview, onClose }) {
  const [expanded, setExpanded] = useState(false)
  const dialogRef = useRef(null)

  useEffect(() => {
    if (!preview) {
      setExpanded(false)
      return undefined
    }

    const previousOverflow = document.body.style.overflow
    const previousActiveElement = document.activeElement
    const closeOnEscape = (event) => {
      if (event.key !== 'Escape') return
      if (expanded) setExpanded(false)
      else onClose?.()
    }

    document.body.style.overflow = 'hidden'
    window.addEventListener('keydown', closeOnEscape)
    const focusFrame = window.requestAnimationFrame(() => dialogRef.current?.querySelector('[data-preview-primary-action]')?.focus())

    return () => {
      window.cancelAnimationFrame(focusFrame)
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', closeOnEscape)
      previousActiveElement?.focus?.()
    }
  }, [expanded, onClose, preview])

  if (!preview) return null

  const title = preview.work.title || '未命名作品'
  const author = preview.work.author || preview.author
  const published = preview.work.is_published

  return (
    <div className="dialog-backdrop teacher-preview-backdrop" role="presentation" onMouseDown={(event) => { if (!expanded && event.target === event.currentTarget) onClose?.() }}>
      <section ref={dialogRef} className={`teacher-preview-dialog${expanded ? ' is-expanded' : ''}`} role="dialog" aria-modal="true" aria-labelledby="teacher-preview-title">
        <header>
          <div className="teacher-preview-heading">
            <div>
              <h2 id="teacher-preview-title">{title}</h2>
              {author ? <span>{author}的作品</span> : null}
            </div>
            <Badge tone={published ? 'success' : 'info'}>{published ? '已发布' : '草稿'}</Badge>
          </div>
          <div className="teacher-preview-actions">
            <Button data-preview-primary-action variant="secondary" icon={expanded ? ArrowsIn : ArrowsOut} onClick={() => setExpanded((value) => !value)}>
              {expanded ? '退出大屏' : '大屏预览'}
            </Button>
            <button className="icon-button" onClick={onClose} aria-label="关闭作品预览"><X size={20} /></button>
          </div>
        </header>
        <div className="teacher-preview-frame">
          <iframe src={preview.url} sandbox="allow-scripts" allow="fullscreen" allowFullScreen title={`预览作品：${title}`} referrerPolicy="no-referrer" />
        </div>
      </section>
    </div>
  )
}
