import { useEffect, useMemo, useState } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { ChalkboardTeacher, MagnifyingGlass, Student } from '@phosphor-icons/react'
import { api } from '../api'
import { useAuth } from '../contexts/AuthContext'
import { Button, Notice } from '../components/UI'

export default function LoginPage() {
  const { user, login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [role, setRole] = useState('student')
  const [classes, setClasses] = useState([])
  const [classId, setClassId] = useState('')
  const [students, setStudents] = useState([])
  const [rosterLoading, setRosterLoading] = useState(false)
  const [loginName, setLoginName] = useState('')
  const [search, setSearch] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => { api('/api/public/classes').then((data) => setClasses(data.classes)).catch((err) => setError(err.message)) }, [])
  useEffect(() => {
    setStudents([]); setLoginName(''); setSearch(''); setError('')
    if (role !== 'student' || !classId) {
      setRosterLoading(false)
      return
    }

    const controller = new AbortController()
    setRosterLoading(true)
    api(`/api/public/classes/${classId}/roster`, { signal: controller.signal })
      .then((data) => setStudents(Array.isArray(data.students) ? data.students : []))
      .catch((err) => {
        if (err.name !== 'AbortError') setError(err.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setRosterLoading(false)
      })

    return () => controller.abort()
  }, [classId, role])

  const filteredStudents = useMemo(() => students.filter((item) => `${item.name} ${item.login_name}`.toLowerCase().includes(search.toLowerCase())), [students, search])
  if (user) return <Navigate to={user.must_change_password ? '/change-password' : user.role === 'teacher' ? '/teacher' : '/studio'} replace />

  async function submit(event) {
    event.preventDefault(); setError(''); setLoading(true)
    try {
      const loggedIn = await login({ role, class_id: Number(classId || 0), login_name: loginName, password })
      if (loggedIn.must_change_password) navigate('/change-password', { replace: true })
      else navigate(location.state?.from || (loggedIn.role === 'teacher' ? '/teacher' : '/studio'), { replace: true })
    } catch (err) { setError(err.message) } finally { setLoading(false) }
  }

  return (
    <main className="login-page">
      <section className="login-intro">
        <div className="login-brand"><img className="brand-logo" src="/code-lab-logo.svg" alt="代码创作实验室标志" /><span>代码创作实验室</span></div>
        <div className="intro-copy"><p className="kicker">把创意变成可以玩的网页</p><h1>今天，我们一起做出第一个作品。</h1><p>和 AI 讨论想法，预览代码效果，再把完成的小游戏分享给同学。</p></div>
        <div className="intro-path"><span>描述创意</span><i /> <span>生成代码</span><i /> <span>运行发布</span></div>
      </section>
      <section className="login-panel">
        <form className="login-card" onSubmit={submit}>
          <header><h2>进入创作空间</h2><p>请选择身份并填写登录信息</p></header>
          <div className="role-switch" aria-label="登录身份">
            <button type="button" className={role === 'student' ? 'active' : ''} onClick={() => { setRole('student'); setLoginName('') }}><Student size={21} />学生</button>
            <button type="button" className={role === 'teacher' ? 'active' : ''} onClick={() => { setRole('teacher'); setLoginName('teacher') }}><ChalkboardTeacher size={21} />教师</button>
          </div>
          {error ? <Notice type="error" title={error} /> : null}
          {role === 'student' ? <>
            <label className="field"><span>班级</span><select value={classId} onChange={(event) => setClassId(event.target.value)} required><option value="">选择自己的班级</option>{classes.map((item) => <option key={item.id} value={item.id} disabled={!item.login_open}>{item.name}{item.login_open ? '' : '（未开放）'}</option>)}</select></label>
            <div className="field">
              <span>姓名</span>
              <div className="search-input"><MagnifyingGlass size={18} /><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索姓名" aria-label="搜索姓名" /></div>
              {!classId ? <p className="field-help">选班级后会显示本班名单</p>
                : rosterLoading ? <p className="field-help">正在加载本班名单...</p>
                  : students.length === 0 ? <p className="field-help">本班暂时没有学生名单，请联系教师。</p>
                    : filteredStudents.length === 0 ? <p className="field-help">没有找到匹配的学生，请清空搜索内容后重试。</p>
                      : <div className="student-picker" role="listbox" aria-label="选择姓名">{filteredStudents.map((item) => <button type="button" role="option" aria-selected={loginName === item.login_name} className={loginName === item.login_name ? 'selected' : ''} key={item.id} onClick={() => setLoginName(item.login_name)}><span>{item.name}</span>{item.login_name !== item.name ? <small>{item.login_name}</small> : null}</button>)}</div>}
            </div>
          </> : <label className="field"><span>教师账号</span><input value={loginName} onChange={(event) => setLoginName(event.target.value)} required /></label>}
          <label className="field"><span>密码</span><input type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="输入密码" autoComplete="current-password" required /></label>
          <Button type="submit" disabled={loading || !loginName}>{loading ? '正在登录' : role === 'student' ? '进入创作空间' : '进入教师后台'}</Button>
          <p className="login-help">首次登录请使用教师发放的初始密码，进入后立即设置自己的新密码。</p>
        </form>
      </section>
    </main>
  )
}
