import { useCallback, useEffect, useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { ApiError, request, type User } from './lib/api'
import AuthPage from './pages/AuthPage'
import DashboardPage from './pages/DashboardPage'
import HomePage from './pages/HomePage'

function AppRoutes() {
  const navigate = useNavigate()
  const location = useLocation()
  const [token, setToken] = useState(() => localStorage.getItem('travo_token'))
  const [user, setUser] = useState<User | null>(() => {
    if (!token) return null
    try {
      const saved = localStorage.getItem('travo_user')
      return saved ? JSON.parse(saved) as User : null
    } catch {
      return null
    }
  })
  const [checkingSession, setCheckingSession] = useState(Boolean(token))

  const updateUser = useCallback((currentUser: User) => {
    setUser(currentUser)
    localStorage.setItem('travo_user', JSON.stringify(currentUser))
  }, [])

  useEffect(() => {
    if (!token) return
    request<{ user: User }>('/auth/me')
      .then(({ user: currentUser }) => {
        updateUser(currentUser)
      })
      .catch((error: Error) => {
        if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
          localStorage.removeItem('travo_token')
          localStorage.removeItem('travo_user')
          setToken(null)
          setUser(null)
        }
      })
      .finally(() => setCheckingSession(false))
  }, [token, updateUser])

  function authenticate(nextToken: string, nextUser: User) {
    localStorage.setItem('travo_token', nextToken)
    localStorage.setItem('travo_user', JSON.stringify(nextUser))
    setToken(nextToken)
    setUser(nextUser)
    setCheckingSession(false)
    navigate('/dashboard', { state: location.state })
  }

  function signOut() {
    localStorage.removeItem('travo_token')
    localStorage.removeItem('travo_user')
    setToken(null)
    setUser(null)
    setCheckingSession(false)
    navigate('/')
  }

  return (
    <Routes>
      <Route
        path="/"
        element={
          <HomePage
            isAuthenticated={Boolean(token && user)}
            userRole={user?.role}
            onLogin={() => navigate('/login')}
            onSignup={() => navigate('/signup')}
            onDashboard={() => navigate('/dashboard')}
            onBookTrip={(tripId) => navigate(
              token && user ? '/dashboard' : '/signup',
              { state: { bookTripId: tripId } },
            )}
          />
        }
      />
      <Route path="/login" element={<AuthPage mode="login" onAuthenticated={authenticate} />} />
      <Route path="/signup" element={<AuthPage mode="signup" onAuthenticated={authenticate} />} />
      <Route
        path="/dashboard"
        element={
          checkingSession
            ? <div className="loading-state">Checking your session…</div>
            : token && user
              ? <DashboardPage user={user} onSignOut={signOut} onUserUpdate={updateUser} />
              : <Navigate to="/login" replace />
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

function App() {
  return (
    <BrowserRouter>
      <AppRoutes />
    </BrowserRouter>
  )
}

export default App
