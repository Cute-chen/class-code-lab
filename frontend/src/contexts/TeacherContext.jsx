import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { api } from '../api'

const TeacherContext = createContext(null)

export function TeacherProvider({ children }) {
  const [classes, setClasses] = useState([])
  const [selectedClassId, setSelectedClassId] = useState(() => Number(sessionStorage.getItem('teacher_class_id') || 0))
  const [loadingClasses, setLoadingClasses] = useState(true)
  const refreshClasses = () => api('/api/teacher/classes').then((data) => {
    setClasses(data.classes)
    setSelectedClassId((current) => {
      const next = data.classes.some((item) => item.id === current) ? current : data.classes[0]?.id || 0
      if (next) sessionStorage.setItem('teacher_class_id', String(next))
      return next
    })
    return data.classes
  })
  useEffect(() => { refreshClasses().finally(() => setLoadingClasses(false)) }, [])
  const value = useMemo(() => ({ classes, selectedClassId, loadingClasses, selectedClass: classes.find((item) => item.id === selectedClassId), setSelectedClassId(id) { setSelectedClassId(Number(id)); sessionStorage.setItem('teacher_class_id', String(id)) }, refreshClasses }), [classes, selectedClassId, loadingClasses])
  return <TeacherContext.Provider value={value}>{children}</TeacherContext.Provider>
}

export const useTeacher = () => useContext(TeacherContext)
