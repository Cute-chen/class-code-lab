import { NavLink, Outlet } from 'react-router-dom'
import { ChartBar, GearSix, Robot, SignOut, Student, Trophy, Scroll } from '@phosphor-icons/react'
import { useAuth } from '../contexts/AuthContext'
import { TeacherProvider, useTeacher } from '../contexts/TeacherContext'

const links = [
  ['/teacher', '课堂总览', ChartBar, true],
  ['/teacher/students', '学生管理', Student],
  ['/teacher/works', '作品管理', Trophy],
  ['/teacher/ai', 'AI 对话与用量', Robot],
  ['/teacher/classes', '班级设置', GearSix],
  ['/teacher/audit', '操作记录', Scroll],
]

function TeacherFrame() {
  const { user, logout } = useAuth()
  const { classes, selectedClassId, setSelectedClassId } = useTeacher()
  return <div className="teacher-shell"><aside className="teacher-sidebar"><div className="teacher-brand"><img className="brand-logo" src="/code-lab-logo.svg" alt="代码创作实验室标志" /><div><strong>代码创作实验室</strong><small>教师工作台</small></div></div><nav>{links.map(([to, label, Icon, end]) => <NavLink key={to} to={to} end={end}><Icon size={20} /><span>{label}</span></NavLink>)}</nav><div className="teacher-account"><div><strong>{user.name}</strong><small>教师账号</small></div><button className="icon-button" onClick={logout} aria-label="退出登录"><SignOut size={20} /></button></div></aside><main className="teacher-main"><header className="teacher-topbar"><label><span>当前班级</span><select value={selectedClassId || ''} onChange={(event) => setSelectedClassId(event.target.value)}><option value="">全部或未创建</option>{classes.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label></header><Outlet /></main></div>
}

export default function TeacherLayout() { return <TeacherProvider><TeacherFrame /></TeacherProvider> }
