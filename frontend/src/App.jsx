import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useAuth } from './contexts/AuthContext'
import LoginPage from './pages/LoginPage'
import ChangePasswordPage from './pages/ChangePasswordPage'
import StudentLayout from './layouts/StudentLayout'
import TeacherLayout from './layouts/TeacherLayout'
import StudioPage from './pages/student/StudioPage'
import GalleryPage from './pages/student/GalleryPage'
import WorkPage from './pages/student/WorkPage'
import OverviewPage from './pages/teacher/OverviewPage'
import StudentsPage from './pages/teacher/StudentsPage'
import WorksPage from './pages/teacher/WorksPage'
import AIPage from './pages/teacher/AIPage'
import ClassesPage from './pages/teacher/ClassesPage'
import AuditPage from './pages/teacher/AuditPage'

function Protected({ role, children }) {
  const { user, loading } = useAuth()
  const location = useLocation()
  if (loading) return <div className="app-loading"><img className="brand-logo" src="/code-lab-logo.svg" alt="代码创作实验室标志" /><p>正在进入代码创作实验室</p></div>
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname }} />
  if (user.must_change_password && location.pathname !== '/change-password') return <Navigate to="/change-password" replace />
  if (role && user.role !== role) return <Navigate to={user.role === 'teacher' ? '/teacher' : '/studio'} replace />
  return children
}

function HomeRedirect() {
  const { user, loading } = useAuth()
  if (loading) return null
  if (!user) return <Navigate to="/login" replace />
  if (user.must_change_password) return <Navigate to="/change-password" replace />
  return <Navigate to={user.role === 'teacher' ? '/teacher' : '/studio'} replace />
}

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<HomeRedirect />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/change-password" element={<Protected><ChangePasswordPage /></Protected>} />
      <Route element={<Protected role="student"><StudentLayout /></Protected>}>
        <Route path="/studio" element={<StudioPage />} />
        <Route path="/gallery" element={<GalleryPage mode="class" />} />
        <Route path="/featured" element={<GalleryPage mode="featured" />} />
        <Route path="/works/:id" element={<WorkPage />} />
      </Route>
      <Route path="/teacher" element={<Protected role="teacher"><TeacherLayout /></Protected>}>
        <Route index element={<OverviewPage />} />
        <Route path="students" element={<StudentsPage />} />
        <Route path="works" element={<WorksPage />} />
        <Route path="ai" element={<AIPage />} />
        <Route path="classes" element={<ClassesPage />} />
        <Route path="audit" element={<AuditPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
