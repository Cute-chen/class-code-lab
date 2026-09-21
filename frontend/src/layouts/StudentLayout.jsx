import { NavLink, Outlet } from 'react-router-dom'
import { Code, Compass, SignOut, Sparkle } from '@phosphor-icons/react'
import { useAuth } from '../contexts/AuthContext'

export default function StudentLayout() {
  const { user, logout } = useAuth()
  return <div className="student-shell"><header className="student-header"><div className="student-header-inner"><NavLink to="/studio" className="app-brand"><img className="brand-logo" src="/code-lab-logo.svg" alt="代码创作实验室标志" /><span>代码创作实验室</span></NavLink><nav aria-label="学生端导航"><NavLink to="/studio"><Code size={19} />我的创作</NavLink><NavLink to="/gallery"><Compass size={19} />本班广场</NavLink><NavLink to="/featured"><Sparkle size={19} />跨班精选</NavLink></nav><div className="user-menu"><span><strong>{user.name}</strong><small>{user.class?.name}</small></span><button className="icon-button" onClick={logout} aria-label="退出登录" title="退出登录"><SignOut size={21} /></button></div></div></header><Outlet /></div>
}
