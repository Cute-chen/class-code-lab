import { useEffect, useMemo, useState } from 'react'
import { Coins, Scales } from '@phosphor-icons/react'
import { api } from '../api'
import { Badge, Button, Dialog, Notice } from './UI'

const scoreReferences = [
  ['创意性', '主题有想法，玩法或表达方式有自己的特色。'],
  ['完成度', '功能完整，操作流程清晰，作品可以顺利体验。'],
  ['互动性', '操作反馈明确，能够让体验者愿意继续尝试。'],
  ['视觉与表达', '页面整洁，配色、文字和效果能服务作品主题。'],
]

export default function ScoreControl({ work, scoring, onSaved }) {
  const current = Number(work.my_score || 0)
  const [value, setValue] = useState(String(current))
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => { setValue(String(current)); setError('') }, [current, work.id])
  const parsed = useMemo(() => Number(value), [value])
  const maximum = Math.max(current, current + Number(scoring?.remaining || 0))
  const valid = value !== '' && Number.isInteger(parsed) && parsed >= 1 && parsed <= maximum

  if (!scoring) return null
  if (work.is_mine) {
    return <div className="score-control score-control-readonly"><Coins size={18} weight="duotone" /><span>自己的作品不可评分</span></div>
  }
  if (!scoring.enabled) {
    return <div className="score-control score-control-readonly"><Coins size={18} weight="duotone" /><span>评分已暂停{current > 0 ? `，你已投 ${current} 分` : ''}</span></div>
  }

  function openDialog() {
    setValue(String(current))
    setError('')
    setOpen(true)
  }

  function closeDialog() {
    if (!saving) setOpen(false)
  }

  async function save() {
    if (!valid || parsed === current || saving) return
    setSaving(true)
    setError('')
    try {
      const data = await api(`/api/gallery/${work.id}/score`, { method: 'PUT', body: { points: parsed } })
      setOpen(false)
      onSaved?.(data)
    } catch (err) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  return <>
    <div className="score-control">
      <div className="score-control-heading"><Coins size={19} weight="duotone" /><strong>{current > 0 ? `已评分 ${current} 分` : '还未评分'}</strong><span>剩余 {scoring.remaining} 分</span></div>
      <Button variant="secondary" icon={Scales} onClick={openDialog}>{current > 0 ? '修改评分' : '开始评分'}</Button>
    </div>
    <Dialog open={open} title={`给「${work.title || '未命名作品'}」评分`} onClose={closeDialog} actions={<><Button variant="ghost" onClick={closeDialog} disabled={saving}>取消</Button><Button icon={Coins} onClick={save} disabled={!valid || parsed === current || saving}>{saving ? '提交中...' : '提交评分'}</Button></>}>
      <div className="score-modal-work"><div><strong>{work.author}</strong><span>当前总积分 {work.score_total || 0} 分 · {work.supporter_count || 0} 人支持</span></div>{work.rank ? <Badge tone={work.rank <= 3 ? 'warning' : 'info'}>当前第 {work.rank} 名</Badge> : null}</div>
      <section className="score-reference"><div className="score-reference-title"><Scales size={20} weight="duotone" /><strong>评分参考标准</strong></div><p>请结合整体体验分配积分，不必平均分配，觉得更优秀的作品可以多给一些。</p><ul>{scoreReferences.map(([title, description]) => <li key={title}><strong>{title}</strong><span>{description}</span></li>)}</ul></section>
      <div className="score-budget-note"><span>你的评分额度</span><strong>总共 {scoring.budget} 分，已使用 {scoring.spent} 分，剩余 {scoring.remaining} 分</strong></div>
      <label className="field score-modal-input"><span>给这个作品的积分</span><div><input type="number" min="1" max={maximum} step="1" value={value} autoFocus onChange={(event) => setValue(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') save() }} /><em>分</em></div><small>本次可填写 1～{maximum} 分；提交后可以修改分值，但不能撤回评分。</small></label>
      {error ? <Notice type="error" title="评分提交失败">{error}</Notice> : null}
    </Dialog>
  </>
}
