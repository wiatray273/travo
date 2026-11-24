import { useEffect, useState, type FormEvent } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { ArrowLeft, CarFront, ShieldCheck } from 'lucide-react'
import { post, request, type Country, type User } from '../lib/api'
import './Platform.css'

interface AuthResponse {
  token: string
  user: User
}

interface AuthPageProps {
  mode: 'login' | 'signup'
  onAuthenticated: (token: string, user: User) => void
}

export default function AuthPage({ mode, onAuthenticated }: AuthPageProps) {
  const location = useLocation()
  const [fullName, setFullName] = useState('')
  const [email, setEmail] = useState('')
  const [phone, setPhone] = useState('')
  const [identifier, setIdentifier] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<'passenger' | 'driver'>('passenger')
  const [countryID, setCountryID] = useState('')
  const [countries, setCountries] = useState<Country[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const isSignup = mode === 'signup'

  useEffect(() => {
    if (!isSignup) return
    request<{ countries: Country[] }>('/countries')
      .then(({ countries: available }) => {
        setCountries(available)
        if (available.length > 0) setCountryID(String(available[0].id))
      })
      .catch((cause: Error) => setError(cause.message))
  }, [isSignup])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    if (isSignup && !email.trim() && !phone.trim()) {
      setError('Enter an email address or phone number.')
      return
    }

    setLoading(true)
    try {
      const result = isSignup
        ? await post<AuthResponse>('/auth/register', {
            full_name: fullName,
            email: email.trim(),
            phone: phone.trim(),
            password,
            role,
            country_id: Number(countryID),
          })
        : await post<AuthResponse>('/auth/login', {
            identifier: identifier.trim(),
            password,
          })
      onAuthenticated(result.token, result.user)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Authentication failed.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <main className="auth-page">
      <Link to="/" className="auth-back"><ArrowLeft size={17} /> Back to Travo</Link>
      <section className="auth-card">
        <Link to="/" className="brand auth-brand" aria-label="Travo home">
          <span className="brand-icon"><CarFront size={24} /></span>
          <span>travo<span className="brand-dot">.</span></span>
        </Link>
        <span className="section-kicker">{isSignup ? 'START YOUR JOURNEY' : 'WELCOME BACK'}</span>
        <h1>{isSignup ? 'Create your account' : 'Log in to Travo'}</h1>
        <p className="auth-description">
          {isSignup
            ? 'Join the community as a passenger or apply to drive.'
            : 'Use the email address or phone number linked to your account.'}
        </p>

        <form className="platform-form" onSubmit={handleSubmit}>
          {isSignup ? (
            <>
              <label>
                Full name
                <input value={fullName} onChange={(event) => setFullName(event.target.value)} minLength={2} required autoComplete="name" />
              </label>
              <div className="platform-form-row">
                <label>
                  Email <span className="optional-label">optional</span>
                  <input type="email" value={email} onChange={(event) => setEmail(event.target.value)} autoComplete="email" />
                </label>
                <label>
                  Phone <span className="optional-label">optional</span>
                  <input type="tel" value={phone} onChange={(event) => setPhone(event.target.value)} autoComplete="tel" />
                </label>
              </div>
              <label>
                I want to
                <select value={role} onChange={(event) => setRole(event.target.value as 'passenger' | 'driver')}>
                  <option value="passenger">Travel as a passenger</option>
                  <option value="driver">Drive and publish trips</option>
                </select>
              </label>
              <label>
                Country
                <select value={countryID} onChange={(event) => setCountryID(event.target.value)} required>
                  {countries.map((country) => <option key={country.id} value={country.id}>{country.name}</option>)}
                </select>
              </label>
            </>
          ) : (
            <label>
              Email or phone number
              <input value={identifier} onChange={(event) => setIdentifier(event.target.value)} autoComplete="username" required />
            </label>
          )}
          <label>
            Password
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              minLength={8}
              maxLength={72}
              autoComplete={isSignup ? 'new-password' : 'current-password'}
              required
            />
            {isSignup && <span className="field-hint">Use 8–72 characters.</span>}
          </label>

          {error && <p className="form-error" role="alert">{error}</p>}
          {isSignup && role === 'driver' && (
            <p className="form-note"><ShieldCheck size={17} /> Driver accounts are reviewed before trips can be published.</p>
          )}

          <button className="platform-primary-button" type="submit" disabled={loading || isSignup && countries.length === 0}>
            {loading ? 'Please wait…' : isSignup ? 'Create account' : 'Log in'}
          </button>
        </form>

        <p className="auth-switch">
          {isSignup ? 'Already have an account?' : 'New to Travo?'}{' '}
          <Link to={isSignup ? '/login' : '/signup'} state={location.state}>{isSignup ? 'Log in' : 'Create an account'}</Link>
        </p>
      </section>
    </main>
  )
}
