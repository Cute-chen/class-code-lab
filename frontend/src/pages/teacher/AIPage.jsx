import { useEffect, useState } from 'react'
import { ArrowClockwise, Brain, ChatCircleDots, CheckCircle, Pulse, WarningCircle } from '@phosphor-icons/react'
import { api } from '../../api'
import { Badge, Button, EmptyState, Notice, PageHeader, Skeleton, formatTime } from '../../components/UI'

const formatNumber = (value) => value == null ? '未提供' : Number(value).toLocaleString('zh-CN')
const formatCacheRate = (value) => value == null ? '未提供' : `${Number(value).toFixed(1)}%`
const displayName = (name, id) => name || `ID ${id}`
const requestModeLabel = { generate_full: '完整生成', conversation_start: '新对话', modify_patch: '增量修改', modify_complex: '复杂修改' }

export default function AIPage() {
  const [tab, setTab] = useState('usage')
  const [usage, setUsage] = useState(null)
  const [conversations, setConversations] = useState(null)
  const [health, setHealth] = useState(null)
  const [notice, setNotice] = useState(null)

  const load = async () => {
    try {
      const [u, c, h] = await Promise.all([
        api('/api/teacher/ai/usage'),
        api('/api/teacher/ai/conversations'),
        api('/api/teacher/ai/health'),
      ])
      setUsage(u)
      setConversations(c.conversations)
      setHealth(h)
    } catch (err) {
      setNotice({ type: 'error', title: err.message })
    }
  }

  useEffect(() => { load() }, [])

  async function test() {
    setNotice(null)
    try {
      const result = await api('/api/teacher/ai/test', { method: 'POST' })
      setNotice({ type: 'success', title: `模型连接正常，耗时 ${result.duration_ms}ms` })
    } catch (err) {
      setNotice({ type: 'error', title: err.message })
    }
  }

  return <div className="teacher-page">
    <PageHeader title="AI 对话与用量" description="查看真实请求状态、Token 用量和 DeepSeek 缓存命中。" actions={<>
      <Button variant="secondary" icon={ArrowClockwise} onClick={load}>刷新</Button>
      <Button icon={Pulse} onClick={test} disabled={!health?.configured}>测试模型连接</Button>
    </>} />
    {notice ? <Notice type={notice.type} title={notice.title} onClose={() => setNotice(null)} /> : null}
    {!usage ? <Skeleton rows={6} /> : <>
      <section className="model-status">
        <span className={health.configured ? 'healthy' : 'unhealthy'}><Brain size={28} weight="duotone" /></span>
        <div>
          <strong>{health.configured ? '模型服务已配置' : '模型服务未配置'}</strong>
          <p>{health.model || '尚未填写 AI_MODEL'}，{health.base_url}</p>
          {health.configured ? <p>完整生成上限 {formatNumber(health.max_output_tokens)} · 增量修改上限 {formatNumber(health.modification_max_tokens)} · 历史最多 {health.history_messages} 条/{formatNumber(health.history_chars)} 字符</p> : null}
        </div>
        <Badge tone={health.configured ? 'success' : 'warning'}>{health.configured ? '可用' : '降级运行'}</Badge>
      </section>

      <div className="teacher-tabs">
        <button className={tab === 'usage' ? 'active' : ''} onClick={() => setTab('usage')}>使用概况</button>
        <button className={tab === 'conversations' ? 'active' : ''} onClick={() => setTab('conversations')}>学生对话</button>
        <button className={tab === 'errors' ? 'active' : ''} onClick={() => setTab('errors')}>异常记录</button>
      </div>

      {tab === 'usage' ? <>
        <div className="metric-grid">
          <article><span><ChatCircleDots size={23} /></span><div><small>总请求</small><strong>{usage.summary.total}</strong></div></article>
          <article><span><CheckCircle size={23} /></span><div><small>成功</small><strong>{usage.summary.success}</strong></div></article>
          <article><span><Brain size={23} /></span><div><small>缓存命中率</small><strong>{formatCacheRate(usage.summary.cache_hit_rate)}</strong></div></article>
          <article><span><WarningCircle size={23} /></span><div><small>失败</small><strong>{usage.summary.failed}</strong></div></article>
        </div>
        <section className="teacher-section">
          <div className="table-wrap"><table>
            <thead><tr><th>时间</th><th>班级</th><th>学生</th><th>模型</th><th>请求模式</th><th>状态</th><th>结束原因</th><th>耗时</th><th>输入 Token</th><th>缓存命中</th><th>缓存未命中</th><th>输出 Token</th></tr></thead>
            <tbody>{usage.logs.map((log) => <tr key={log.id}>
              <td>{formatTime(log.created_at)}</td><td>{displayName(log.class_name, log.class_id)}</td><td>{displayName(log.student_name, log.user_id)}</td><td>{log.model || '未提供'}</td>
              <td>{requestModeLabel[log.request_mode] || log.request_mode || '未提供'}{log.reasoning_effort ? ` · ${log.reasoning_effort}/${formatNumber(log.max_output_tokens)}` : ''}</td>
              <td><Badge tone={log.status === 'success' ? 'success' : log.status === 'failed' ? 'danger' : 'warning'}>{log.status}</Badge></td>
              <td>{log.finish_reason || '未提供'}</td>
              <td>{log.duration_ms ? `${log.duration_ms}ms` : '未提供'}</td><td>{formatNumber(log.prompt_tokens)}</td>
              <td>{formatNumber(log.prompt_cache_hit_tokens)}</td><td>{formatNumber(log.prompt_cache_miss_tokens)}</td><td>{formatNumber(log.completion_tokens)}</td>
            </tr>)}</tbody>
          </table></div>
        </section>
      </> : tab === 'conversations' ? <section className="conversation-audit">
        {conversations?.length ? conversations.map((conversation) => <details key={conversation.id}>
          <summary><div><strong>{conversation.title || '未命名对话'}</strong><span>{displayName(conversation.class_name, conversation.class_id)} · {displayName(conversation.student_name, conversation.user_id)}，{formatTime(conversation.updated_at)}</span></div><Badge>{conversation.messages?.length || 0} 条消息</Badge></summary>
          <div>{conversation.messages?.map((message) => <article key={message.id} className={message.role}><strong>{message.role === 'user' ? '学生' : 'AI'}</strong><p>{message.content}</p></article>)}</div>
        </details>) : <EmptyState title="还没有学生对话" description="学生使用 AI 助手后会显示在这里。" />}
      </section> : <section className="teacher-section">
        {usage.logs.filter((log) => log.status === 'failed').length ? <div className="table-wrap"><table>
          <thead><tr><th>时间</th><th>班级</th><th>学生</th><th>错误类别</th><th>模型</th><th>耗时</th></tr></thead>
          <tbody>{usage.logs.filter((log) => log.status === 'failed').map((log) => <tr key={log.id}><td>{formatTime(log.created_at)}</td><td>{displayName(log.class_name, log.class_id)}</td><td>{displayName(log.student_name, log.user_id)}</td><td><Badge tone="danger">{log.error_category || '未知'}</Badge></td><td>{log.model}</td><td>{log.duration_ms}ms</td></tr>)}</tbody>
        </table></div> : <EmptyState title="近期没有异常请求" description="鉴权失败、限流、超时和上游错误会出现在这里。" />}
      </section>}
    </>}
  </div>
}
