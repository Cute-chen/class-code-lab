import { useEffect, useMemo, useState } from 'react'
import { MagnifyingGlass } from '@phosphor-icons/react'
import { api } from '../../api'
import { Badge, EmptyState, Notice, PageHeader, Skeleton, formatTime } from '../../components/UI'

const actionLabels = { login: '登录', login_failed: '登录失败', change_password: '修改密码', reset_password: '重置密码', publish: '发布', unpublish: '撤下', feature: '加入精选', unfeature: '取消精选', lock: '锁定', unlock: '解锁', create: '创建', import: '导入名单', update_settings: '修改设置', update_status: '修改学生状态' }

export default function AuditPage() {
  const [logs, setLogs] = useState(null)
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [error, setError] = useState('')
  useEffect(() => { api('/api/teacher/audit-logs').then((data) => setLogs(data.logs)).catch((err) => setError(err.message)) }, [])
  const filtered = useMemo(() => (logs || []).filter((item) => `${item.actor_name} ${item.action} ${item.target} ${item.detail}`.toLowerCase().includes(search.toLowerCase())).filter((item) => filter === 'all' || item.action === filter), [logs, search, filter])
  return <div className="teacher-page"><PageHeader title="操作记录" description="这里只记录关键操作，不保存密码、密钥、Cookie 或完整代码。" />{error ? <Notice type="error" title={error} /> : null}{logs === null ? <Skeleton rows={7} /> : logs.length === 0 ? <EmptyState title="还没有操作记录" description="登录、改密、发布、撤下和管理操作会出现在这里。" /> : <section className="teacher-section"><div className="table-tools"><label className="search-input"><MagnifyingGlass size={18} /><input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索操作者、动作或备注" /></label><select value={filter} onChange={(e) => setFilter(e.target.value)}><option value="all">全部操作</option><option value="login">登录</option><option value="change_password">改密</option><option value="reset_password">密码重置</option><option value="publish">发布</option><option value="unpublish">撤下</option><option value="feature">精选</option><option value="lock">锁定</option></select></div><div className="table-wrap"><table><thead><tr><th>时间</th><th>操作者</th><th>班级 ID</th><th>对象</th><th>动作</th><th>结果</th><th>说明</th></tr></thead><tbody>{filtered.map((item) => <tr key={item.id}><td>{formatTime(item.created_at)}</td><td>{item.actor_name || '未登录用户'}</td><td>{item.class_id ?? '全局'}</td><td>{item.target}</td><td>{actionLabels[item.action] || item.action}</td><td><Badge tone={item.result === 'success' ? 'success' : 'danger'}>{item.result}</Badge></td><td>{item.detail || '无'}</td></tr>)}</tbody></table></div></section>}</div>
}
