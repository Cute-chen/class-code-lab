import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { api } from '../api'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api('/api/auth/me')
      .then((data) => setUser(data.user))
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
  }, [])

  const value = useMemo(() => ({
    user,
    loading,
    async login(payload) {
      const data = await api('/api/auth/login', { method: 'POST', body: payload })
      setUser(data.user)
      return data.user
    },
    async logout() {
      try { await api('/api/auth/logout', { method: 'POST' }) } finally { setUser(null) }
    },
    refresh() {
      return api('/api/auth/me').then((data) => { setUser(data.user); return data.user })
    },
  }), [user, loading])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export const useAuth = () => useContext(AuthContext)
