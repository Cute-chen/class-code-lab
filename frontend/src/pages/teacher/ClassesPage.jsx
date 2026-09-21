import { useState } from 'react'
import { Plus } from '@phosphor-icons/react'
import { api } from '../../api'
import { useTeacher } from '../../contexts/TeacherContext'
import { Badge, Button, Dialog, EmptyState, Notice, PageHeader, Skeleton } from '../../components/UI'

export default function ClassesPage() {
  const { classes, selectedClassId, setSelectedClassId, refreshClasses, loadingClasses } = useTeacher()
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState('')
  const [notice, setNotice] = useState(null)

  async function create() {
    try { const data = await api('/api/teacher/classes', { method: 'POST', body: { name } }); await refreshClasses(); setSelectedClassId(data.class.id); setCreateOpen(false); setName(''); setNotice({ type: 'success', title: '班级已创建' }) }
    catch (err) { setNotice({ type: 'error', title: err.message }) }
  }
  async function update(item, body) {
    try { await api(`/api/teacher/classes/${item.id}`, { method: 'PATCH', body }); await refreshClasses(); setNotice({ type: 'success', title: `${item.name} 设置已保存` }) }
    catch (err) { setNotice({ type: 'error', title: err.message }) }
  }

  return <div className="teacher-page">
    <PageHeader title="班级设置" description="控制登录、AI、发布和课堂额度。" actions={<Button icon={Plus} onClick={() => setCreateOpen(true)}>创建班级</Button>} />
    {notice ? <Notice type={notice.type} title={notice.title} onClose={() => setNotice(null)} /> : null}
    {loadingClasses ? <Skeleton rows={6} /> : classes.length === 0 ? <EmptyState title="创建第一个班级" description="班级是学生、作品和 AI 用量的隔离边界。" action={<Button icon={Plus} onClick={() => setCreateOpen(true)}>创建班级</Button>} /> : <div className="class-settings-list">{classes.map((item) => <article key={item.id} className={item.id === selectedClassId ? 'selected' : ''}>
      <header><div><h2>{item.name}</h2><span>{item.active ? '正常使用' : '已停用'}</span></div><Badge tone={item.login_open ? 'success' : 'warning'}>{item.login_open ? '登录开放' : '登录关闭'}</Badge></header>
      <div className="settings-grid">
        <label className="switch-row"><span>班级启用<small>停用后保留数据但不再使用</small></span><input type="checkbox" checked={item.active} onChange={(e) => update(item, { active: e.target.checked })} /></label>
        <label className="switch-row"><span>允许学生登录<small>上课前开放，课后关闭</small></span><input type="checkbox" checked={item.login_open} onChange={(e) => update(item, { login_open: e.target.checked })} /></label>
        <label className="switch-row"><span>开启 AI 助手<small>关闭后保留手动编辑能力</small></span><input type="checkbox" checked={item.ai_enabled} onChange={(e) => update(item, { ai_enabled: e.target.checked })} /></label>
        <label className="switch-row"><span>允许发布<small>控制学生是否可更新广场作品</small></span><input type="checkbox" checked={item.publish_enabled} onChange={(e) => update(item, { publish_enabled: e.target.checked })} /></label>
        <label className="switch-row"><span>允许修改已发布作品<small>再次发布前不影响公开快照</small></span><input type="checkbox" checked={item.allow_edit_published} onChange={(e) => update(item, { allow_edit_published: e.target.checked })} /></label>
        <label className="field"><span>每名学生 AI 次数</span><input type="number" min="0" defaultValue={item.ai_request_limit} onBlur={(e) => update(item, { ai_request_limit: Number(e.target.value) })} /></label>
        <label className="field"><span>每名学生作品评分积分<small>大于 0 自动开放评分，设为 0 时暂停并保留榜单</small></span><input type="number" min="0" step="1" defaultValue={item.score_budget || 0} onBlur={(e) => update(item, { score_budget: Number(e.target.value) })} /></label>
        <label className="field"><span>班级 AI 并发数</span><input type="number" min="1" max="20" defaultValue={item.ai_concurrency} onBlur={(e) => update(item, { ai_concurrency: Number(e.target.value) })} /></label>
        <label className="field"><span>请求冷却时间（秒）</span><input type="number" min="0" max="120" defaultValue={item.ai_request_cooldown_seconds} onBlur={(e) => update(item, { ai_request_cooldown_seconds: Number(e.target.value) })} /></label>
      </div>
    </article>)}</div>}
    <Dialog open={createOpen} title="创建班级" onClose={() => setCreateOpen(false)} actions={<><Button variant="ghost" onClick={() => setCreateOpen(false)}>取消</Button><Button icon={Plus} disabled={!name.trim()} onClick={create}>确认创建</Button></>}><label className="field"><span>班级名称</span><input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：七年级 1 班" autoFocus /></label><Notice type="info" title="班级隔离">学生默认只能看到本班普通作品，教师推荐的精选作品除外。</Notice></Dialog>
  </div>
}
