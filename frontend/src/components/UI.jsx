import { CheckCircle, Info, Warning, X, XCircle } from '@phosphor-icons/react'

export function Button({ variant = 'primary', icon: Icon, children, className = '', ...props }) {
  return (
    <button className={`button button-${variant} ${className}`} {...props}>
      {Icon ? <Icon size={18} weight="bold" aria-hidden="true" /> : null}
      <span>{children}</span>
    </button>
  )
}

export function Notice({ type = 'info', title, children, onClose }) {
  const icons = { info: Info, success: CheckCircle, warning: Warning, error: XCircle }
  const Icon = icons[type] || Info
  return (
    <div className={`notice notice-${type}`} role={type === 'error' ? 'alert' : 'status'}>
      <Icon size={21} weight="fill" aria-hidden="true" />
      <div><strong>{title}</strong>{children ? <div>{children}</div> : null}</div>
      {onClose ? <button className="icon-button" onClick={onClose} aria-label="关闭提示"><X size={18} /></button> : null}
    </div>
  )
}

export function Badge({ tone = 'neutral', children }) {
  return <span className={`badge badge-${tone}`}>{children}</span>
}

export function Skeleton({ rows = 4 }) {
  return <div className="skeleton" aria-label="正在加载">{Array.from({ length: rows }).map((_, index) => <div key={index} className="skeleton-line" />)}</div>
}

export function EmptyState({ icon: Icon, title, description, action }) {
  return (
    <div className="empty-state">
      {Icon ? <span className="empty-icon"><Icon size={30} weight="duotone" /></span> : null}
      <h3>{title}</h3><p>{description}</p>{action}
    </div>
  )
}

export function Dialog({ open, title, children, actions, onClose }) {
  if (!open) return null
  return (
    <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose?.() }}>
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title">
        <header><h2 id="dialog-title">{title}</h2><button className="icon-button" onClick={onClose} aria-label="关闭"><X size={20} /></button></header>
        <div className="dialog-body">{children}</div>
        {actions ? <footer>{actions}</footer> : null}
      </div>
    </div>
  )
}

export function PageHeader({ title, description, actions }) {
  return <div className="page-header"><div><h1>{title}</h1>{description ? <p>{description}</p> : null}</div>{actions ? <div className="page-actions">{actions}</div> : null}</div>
}

export function formatTime(value) {
  if (!value) return '暂无'
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}
