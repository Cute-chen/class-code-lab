import { useEffect, useState } from 'react'
import { ArrowClockwise, Plus, Pulse } from '@phosphor-icons/react'
import { api } from '../../api'
import { Badge, Button, Dialog, EmptyState, Notice } from '../../components/UI'

const freshProvider = {
  name: '', base_url: 'https://api.openai.com/v1', api_key: '', model: '', enabled: true,
  max_concurrency: 6, timeout_seconds: 600, max_output_tokens: 393216,
  modification_max_tokens: 393216, reasoning_effort: 'auto',
}
const numericProviderFields = ['max_concurrency', 'timeout_seconds', 'max_output_tokens', 'modification_max_tokens']
const statusText = { ready: '可用', disabled: '已停用', full: '满载', cooldown: '冷却中', authentication: '鉴权失败' }

export default function AIConfigPanel({ onChanged }) {
  const [providers, setProviders] = useState(null)
  const [settings, setSettings] = useState(null)
  const [editing, setEditing] = useState(null)
  const [draft, setDraft] = useState(freshProvider)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState(null)

  async function load() {
    try {
      const [p, s] = await Promise.all([api('/api/teacher/ai/providers'), api('/api/teacher/ai/settings')])
      setProviders(p.providers)
      setSettings(s.settings)
    } catch (err) { setNotice({ type: 'error', title: err.message }) }
  }
  useEffect(() => { load() }, [])

  function open(provider) {
    setEditing(provider?.id || 'new')
    setDraft(provider ? { ...provider, api_key: '' } : { ...freshProvider })
  }

  async function saveProvider(event) {
    event.preventDefault()
    setBusy(true)
    setNotice(null)
    try {
      const body = Object.fromEntries(['name', 'base_url', 'api_key', 'model', 'enabled', 'max_concurrency', 'timeout_seconds', 'max_output_tokens', 'modification_max_tokens', 'reasoning_effort'].map(key => [key, draft[key]]))
      for (const key of numericProviderFields) body[key] = Number(body[key])
      const path = editing === 'new' ? '/api/teacher/ai/providers' : `/api/teacher/ai/providers/${editing}`
      await api(path, { method: editing === 'new' ? 'POST' : 'PATCH', body })
      setEditing(null)
      setNotice({ type: 'success', title: '服务配置已保存' })
      await load()
      onChanged?.()
    } catch (err) { setNotice({ type: 'error', title: err.message }) }
    finally { setBusy(false) }
  }

  async function test(provider) {
    setBusy(true)
    setNotice(null)
    try {
      const result = await api(`/api/teacher/ai/providers/${provider.id}/test`, { method: 'POST' })
      setNotice({ type: 'success', title: `${provider.name} 连接正常，耗时 ${result.duration_ms}ms` })
      await load()
    } catch (err) { setNotice({ type: 'error', title: `${provider.name}：${err.message}` }); await load() }
    finally { setBusy(false) }
  }

  async function toggle(provider) {
    setBusy(true)
    setNotice(null)
    try {
      await api(`/api/teacher/ai/providers/${provider.id}`, { method: 'PATCH', body: { enabled: !provider.enabled } })
      await load()
      onChanged?.()
    } catch (err) { setNotice({ type: 'error', title: err.message }) }
    finally { setBusy(false) }
  }

  async function saveSettings(event) {
    event.preventDefault()
    setBusy(true)
    setNotice(null)
    try {
      const body = Object.fromEntries(['default_class_concurrency', 'default_student_requests', 'history_messages', 'history_chars'].map(key => [key, Number(settings[key])]))
      const result = await api('/api/teacher/ai/settings', { method: 'PATCH', body })
      setSettings(result.settings)
      setNotice({ type: 'success', title: '全局默认设置已保存' })
      onChanged?.()
    } catch (err) { setNotice({ type: 'error', title: err.message }) }
    finally { setBusy(false) }
  }

  return <div className="ai-config-panel">
    {notice ? <Notice type={notice.type} title={notice.title} onClose={() => setNotice(null)} /> : null}
    <section className="teacher-section">
      <header className="ai-config-section-header"><div><h2>服务配置</h2><p>学生请求会分配到有空闲容量的服务。停用后不再接收新请求。</p></div><div className="ai-provider-actions"><Button variant="secondary" icon={ArrowClockwise} onClick={load}>刷新</Button><Button icon={Plus} onClick={() => open(null)}>新增服务</Button></div></header>
      {providers?.length ? <div className="ai-provider-grid">{providers.map(provider => <article className="ai-provider-card" key={provider.id}>
        <div className="ai-provider-card-head"><strong>{provider.name}</strong><Badge tone={provider.status === 'ready' ? 'success' : provider.status === 'disabled' ? 'neutral' : 'warning'}>{statusText[provider.status] || provider.status}</Badge></div>
        <p>{provider.model} · {provider.base_url}</p>
        <div className="ai-provider-stats"><span>运行中 {provider.active_requests}/{provider.max_concurrency}</span><span>请求 {provider.total_requests}</span><span>失败 {provider.failed_requests}</span></div>
        <div className="ai-provider-actions"><Button variant="secondary" onClick={() => open(provider)}>编辑</Button><Button variant="ghost" icon={Pulse} onClick={() => test(provider)} disabled={busy}>测试</Button><Button variant="ghost" onClick={() => toggle(provider)} disabled={busy}>{provider.enabled ? '停用' : '启用'}</Button></div>
      </article>)}</div> : <EmptyState title="还没有 AI 服务" description="新增一组兼容 Chat Completions 的服务，学生即可使用 AI 助手。" action={<Button icon={Plus} onClick={() => open(null)}>新增服务</Button>} />}
    </section>

    {settings ? <section className="teacher-section"><header className="ai-config-section-header"><div><h2>全局默认设置</h2><p>班级默认值只用于之后新建的班级，已有班级仍按自己的设置运行。</p></div></header>
      <form className="ai-config-form" onSubmit={saveSettings}>
        {[['default_class_concurrency', '新班级 AI 并发数', 1, 100], ['default_student_requests', '新班级每名学生请求数', 1, 1000], ['history_messages', '历史消息条数', 1, 100], ['history_chars', '历史字符上限', 100, 1000000]].map(([key, label, min, max]) => <label className="field" key={key}><span>{label}</span><input type="number" min={min} max={max} required value={settings[key]} onChange={event => setSettings({ ...settings, [key]: event.target.value })} /></label>)}
        <Button type="submit" disabled={busy}>保存全局设置</Button>
      </form>
    </section> : null}

    <Dialog open={editing !== null} title={editing === 'new' ? '新增 AI 服务' : '编辑 AI 服务'} onClose={() => setEditing(null)} actions={<><Button variant="ghost" onClick={() => setEditing(null)}>取消</Button><Button type="submit" form="ai-provider-form" disabled={busy}>保存</Button></>}>
      <form id="ai-provider-form" className="ai-config-form" onSubmit={saveProvider}>
        {[['name', '服务名称', 'text'], ['base_url', '接口地址（含 /v1）', 'url'], ['api_key', 'API Key', 'password'], ['model', '模型名称', 'text']].map(([key, label, type]) => <label className="field" key={key}><span>{label}</span><input type={type} required={key !== 'api_key' || editing === 'new'} value={draft[key] || ''} placeholder={key === 'api_key' && editing !== 'new' ? '留空则保留原密钥' : ''} onChange={event => setDraft({ ...draft, [key]: event.target.value })} /></label>)}
        <div className="ai-config-fields">{[['max_concurrency', '最大并发', 1, 100], ['timeout_seconds', '超时（秒）', 1, 3600], ['max_output_tokens', '完整输出上限', 1, 1000000], ['modification_max_tokens', '增量修改上限', 1, 1000000]].map(([key, label, min, max]) => <label className="field" key={key}><span>{label}</span><input type="number" min={min} max={max} required value={draft[key]} onChange={event => setDraft({ ...draft, [key]: event.target.value })} /></label>)}</div>
        <label className="field"><span>思考强度</span><small>自动模式会根据任务复杂度选择 none、low 或 high，兼顾速度和质量。</small><select value={draft.reasoning_effort} onChange={event => setDraft({ ...draft, reasoning_effort: event.target.value })}><option value="auto">自动（推荐）</option><option value="none">none（最快）</option><option value="low">low</option><option value="high">high</option><option value="max">max</option></select></label>
        <label className="ai-config-check"><input type="checkbox" checked={draft.enabled} onChange={event => setDraft({ ...draft, enabled: event.target.checked })} /> 启用此服务</label>
      </form>
    </Dialog>
  </div>
}
