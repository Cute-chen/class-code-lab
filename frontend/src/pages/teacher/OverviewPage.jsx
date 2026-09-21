import { useEffect, useMemo, useState } from 'react'
import { ArrowClockwise, Brain, CheckCircle, DoorOpen, Student, Trophy, Warning } from '@phosphor-icons/react'
import { api } from '../../api'
import { useTeacher } from '../../contexts/TeacherContext'
import { Badge, Button, EmptyState, Notice, PageHeader, Skeleton } from '../../components/UI'

export default function OverviewPage() {
  const { selectedClassId, classes, refreshClasses } = useTeacher()
  const [data, setData] = useState(null)
  const [students, setStudents] = useState(null)
  const [error, setError] = useState('')
  const load = async () => { setError(''); try { const overview = await api('/api/teacher/overview'); setData(overview); if (selectedClassId) { const list = await api(`/api/teacher/classes/${selectedClassId}/students`); setStudents(list.students) } else setStudents([]) } catch (err) { setError(err.message) } }
  useEffect(() => { load(); const timer = setInterval(load, 10000); return () => clearInterval(timer) }, [selectedClassId])
  const selectedClass = classes.find((item) => item.id === selectedClassId)
  const stat = data?.stats?.find((item) => item.class_id === selectedClassId)
  const cards = useMemo(() => [
    ['班级登录', selectedClass?.login_open ? '已开放' : '已关闭', DoorOpen, selectedClass?.login_open ? 'success' : 'warning'],
    ['AI 助手', selectedClass?.ai_enabled ? '已开放' : '已暂停', Brain, selectedClass?.ai_enabled ? 'success' : 'warning'],
    ['登录人数', `${stat?.login_count || 0} / ${stat?.student_count || 0}`, Student, 'info'],
    ['已发布作品', stat?.published || 0, Trophy, 'success'],
  ], [selectedClass, stat])
  async function toggle(field, value) { if (!selectedClassId) return; await api(`/api/teacher/classes/${selectedClassId}`, { method: 'PATCH', body: { [field]: value } }); await refreshClasses(); load() }
  return <div className="teacher-page"><PageHeader title="课堂总览" description="优先处理登录、改密、作品和模型异常。" actions={<Button variant="secondary" icon={ArrowClockwise} onClick={load}>刷新</Button>} />{error ? <Notice type="error" title={error} /> : null}{!selectedClassId ? <EmptyState title="先创建或选择班级" description="进入班级设置创建第一个班级，再导入学生名单。" /> : !data ? <Skeleton rows={6} /> : <><div className="class-control-bar"><div><strong>{selectedClass.name}</strong><span>课堂开关</span></div><div><label className="switch-row"><span>允许登录</span><input type="checkbox" checked={selectedClass.login_open} onChange={(e) => toggle('login_open', e.target.checked)} /></label><label className="switch-row"><span>开启 AI</span><input type="checkbox" checked={selectedClass.ai_enabled} onChange={(e) => toggle('ai_enabled', e.target.checked)} /></label></div></div>{!data.ai_configured ? <Notice type="warning" title="模型服务未配置">学生仍可手动粘贴、编辑、预览和发布代码。</Notice> : null}<div className="metric-grid">{cards.map(([label, value, Icon, tone]) => <article key={label}><span><Icon size={23} weight="duotone" /></span><div><small>{label}</small><strong>{value}</strong></div><Badge tone={tone}>{label === '登录人数' || label === '已发布作品' ? '实时状态' : value}</Badge></article>)}</div><section className="teacher-section"><div className="section-heading"><div><h2>学生进度</h2><p>每 10 秒自动刷新一次。</p></div></div>{students === null ? <Skeleton rows={5} /> : students.length === 0 ? <EmptyState icon={Student} title="还没有学生" description="请到学生管理导入 Excel 名单。" /> : <div className="table-wrap"><table><thead><tr><th>学生</th><th>登录与改密</th><th>作品状态</th><th>AI 状态</th><th>需要关注</th></tr></thead><tbody>{students.map((item) => { const attention = item.locked || item.must_change_password || !item.last_login_at; return <tr key={item.id}><td><strong>{item.name}</strong></td><td><Badge tone={item.must_change_password ? 'warning' : 'success'}>{item.last_login_at ? (item.must_change_password ? '待改密' : '已登录') : '未登录'}</Badge></td><td>{item.work?.is_published ? <Badge tone="success">已发布</Badge> : item.work ? <Badge tone="info">有草稿</Badge> : <Badge>未开始</Badge>}</td><td>{item.ai_blocked ? <Badge tone="danger">已暂停</Badge> : <Badge tone="success">可使用</Badge>}</td><td>{attention ? <span className="attention"><Warning size={17} />需要关注</span> : <span className="normal"><CheckCircle size={17} />正常</span>}</td></tr> })}</tbody></table></div>}</section></>}</div>
}
