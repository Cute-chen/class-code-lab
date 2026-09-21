import { useMemo, useState } from 'react'
import { LockKey, ShieldCheck } from '@phosphor-icons/react'
import { api } from '../api'
import { useAuth } from '../contexts/AuthContext'
import { Button, Notice } from '../components/UI'

export default function ChangePasswordPage() {
  const { user, logout } = useAuth()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const checks = useMemo(() => ({ length: next.length >= 6, different: next !== '123456', match: next !== '' && next === confirm }), [next, confirm])
  const valid = Object.values(checks).every(Boolean) && current

  async function submit(event) {
    event.preventDefault(); setError(''); setLoading(true)
    try { await api('/api/auth/change-password', { method: 'POST', body: { current_password: current, new_password: next } }); await logout(); window.location.assign('/login') }
    catch (err) { setError(err.message); setLoading(false) }
  }

  return <main className="password-page"><form className="password-card" onSubmit={submit}><span className="large-icon"><ShieldCheck size={38} weight="duotone" /></span><h1>先保护好你的作品</h1><p>为了防止别人进入你的账号，请设置一个只有自己知道的新密码。</p>{error ? <Notice type="error" title={error} /> : null}<label className="field"><span>当前初始密码</span><input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required /></label><label className="field"><span>新密码</span><input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" required /></label><label className="field"><span>再次输入新密码</span><input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" required /></label><ul className="password-checks"><li className={checks.length ? 'passed' : ''}>至少 6 位</li><li className={checks.different ? 'passed' : ''}>不能继续使用 123456</li><li className={checks.match ? 'passed' : ''}>两次输入一致</li></ul><Button type="submit" icon={LockKey} disabled={!valid || loading}>{loading ? '正在保存' : '设置新密码'}</Button><small>{user?.name}，修改成功后请使用新密码重新登录。</small></form></main>
}
