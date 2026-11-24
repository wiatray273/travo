
import { useState } from 'react'
import {
  ArrowLeftRight,
  ArrowRight,
  CalendarDays,
  CarFront,
  CheckCircle2,
  Globe2,
  MapPin,
  Menu,
  Search,
  ShieldCheck,
  Users,
  X,
} from 'lucide-react'
import '../App.css'
import { request, type UserRole } from '../lib/api'

interface HomePageProps {
  isAuthenticated: boolean
  userRole?: UserRole
  onLogin: () => void
  onSignup: () => void
  onDashboard: () => void
  onBookTrip: (tripId: number) => void
}

interface PublicTrip {
  id: number
  origin: string
  destination: string
  departure_at: string
  available_seats: number
  price_per_seat: number
  driver_name: string
}

type Language = 'en' | 'fr'

const translations = {
  en: {
    home: 'Home',
    how: 'How it works',
    about: 'About us',
    help: 'Help',
    login: 'Log in',
    signup: 'Sign up',
    badge: 'Travel together. Go further.',
    title1: 'Your journey,',
    title2: 'your way.',
    description:
      'Discover affordable rides, meet fellow travellers and explore Cameroon with Travo.',
    from: 'Leaving from',
    to: 'Going to',
    date: 'Travel date',
    passengers: 'Passengers',
    search: 'Find a ride',
    popular: 'Popular destinations',
    popularDescription: 'Explore some of the most travelled routes in Cameroon.',
    safety: 'Travel with confidence',
    safetyDescription:
      'Your safety matters. Verified drivers, approved pickup points and support throughout your journey.',
    verified: 'Verified drivers',
    verifiedText: 'Drivers are reviewed before they can publish trips.',
    safe: 'Safe pickup points',
    safeText: 'Travel from approved pickup and drop-off locations.',
    support: 'Support when needed',
    supportText: 'Get help when you need it during your journey.',
    howTitle: 'Travel made simple',
    step1: 'Find your ride',
    step1Text: 'Search for available trips between your preferred destinations.',
    step2: 'Book your seat',
    step2Text: 'Choose a ride and reserve your seat.',
    step3: 'Enjoy the journey',
    step3Text: 'Meet your driver at the approved pickup point and travel.',
    footer: 'Making journeys easier, one ride at a time.',
    searchNotice: 'Available trips matching your route are shown below.',
    chooseLocations: 'Please choose two different locations.',
  },
  fr: {
    home: 'Accueil',
    how: 'Comment ça marche',
    about: 'À propos',
    help: 'Aide',
    login: 'Connexion',
    signup: 'Créer un compte',
    badge: 'Voyagez ensemble. Allez plus loin.',
    title1: 'Votre voyage,',
    title2: 'à votre façon.',
    description:
      'Découvrez des trajets abordables, rencontrez d’autres voyageurs et explorez le Cameroun avec Travo.',
    from: 'Départ',
    to: 'Destination',
    date: 'Date du voyage',
    passengers: 'Passagers',
    search: 'Trouver un trajet',
    popular: 'Destinations populaires',
    popularDescription: 'Découvrez quelques-uns des itinéraires les plus fréquentés du Cameroun.',
    safety: 'Voyagez en toute confiance',
    safetyDescription:
      'Votre sécurité est importante. Des conducteurs vérifiés, des points de rencontre approuvés et une assistance pendant votre voyage.',
    verified: 'Conducteurs vérifiés',
    verifiedText: 'Les conducteurs sont contrôlés avant de publier leurs trajets.',
    safe: 'Points de rencontre sûrs',
    safeText: 'Voyagez depuis des lieux de prise en charge approuvés.',
    support: 'Assistance disponible',
    supportText: 'Obtenez de l’aide lorsque vous en avez besoin.',
    howTitle: 'Voyager en toute simplicité',
    step1: 'Trouvez votre trajet',
    step1Text: 'Recherchez des trajets disponibles entre vos destinations.',
    step2: 'Réservez votre place',
    step2Text: 'Choisissez un trajet et réservez votre siège.',
    step3: 'Profitez du voyage',
    step3Text: 'Retrouvez votre conducteur au point de rencontre approuvé.',
    footer: 'Simplifier vos déplacements, un trajet à la fois.',
    searchNotice: 'Les trajets disponibles pour votre itinéraire sont affichés ci-dessous.',
    chooseLocations: 'Veuillez choisir deux lieux différents.',
  },
}

const cities = [
  'Douala',
  'Yaoundé',
  'Bamenda',
  'Buea',
  'Limbe',
  'Bafoussam',
  'Kribi',
  'Garoua',
  'Maroua',
  'Ngaoundéré',
  'Kumba',
  'Tiko',
]

const routes = [
  { from: 'Douala', to: 'Yaoundé', price: 'From 3,000 XAF', time: '3–4 hours' },
  { from: 'Douala', to: 'Buea', price: 'From 2,000 XAF', time: '1–2 hours' },
  { from: 'Yaoundé', to: 'Bafoussam', price: 'From 5,000 XAF', time: '4–5 hours' },
  { from: 'Bamenda', to: 'Douala', price: 'From 5,000 XAF', time: '5–6 hours' },
]

function App({ isAuthenticated, userRole, onLogin, onSignup, onDashboard, onBookTrip }: HomePageProps) {
  const [language, setLanguage] = useState<Language>('en')
  const [origin, setOrigin] = useState('Douala')
  const [destination, setDestination] = useState('Yaoundé')
  const [date, setDate] = useState('')
  const [passengers, setPassengers] = useState(1)
  const [notice, setNotice] = useState('')
  const [mobileMenu, setMobileMenu] = useState(false)
  const [availableTrips, setAvailableTrips] = useState<PublicTrip[]>([])
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState('')
  const [hasSearched, setHasSearched] = useState(false)

  const t = translations[language]
  const today = new Date().toISOString().split('T')[0]

  function swapLocations() {
    setOrigin(destination)
    setDestination(origin)
  }

  async function handleSearch() {
    if (!origin || !destination || origin === destination) {
      setNotice(t.chooseLocations)
      return
    }

    setSearching(true)
    setSearchError('')
    try {
      const params = new URLSearchParams({ origin, destination })
      if (date) params.set('date', date)
      const result = await request<{ trips: PublicTrip[] }>(`/trips?${params.toString()}`)
      setAvailableTrips(result.trips.filter((trip) => trip.available_seats >= passengers))
      setHasSearched(true)
      setNotice(`${origin} → ${destination}. ${t.searchNotice}`)
    } catch (error) {
      setSearchError(error instanceof Error ? error.message : 'Could not search trips.')
      setAvailableTrips([])
      setHasSearched(false)
    } finally {
      setSearching(false)
    }
  }

  return (
    <div className="app-shell">
      <header className="navbar">
        <a className="brand" href="#home" aria-label="Travo home">
          <span className="brand-icon">
            <CarFront size={25} strokeWidth={2.5} />
          </span>
          <span>travo<span className="brand-dot">.</span></span>
        </a>

        <nav className={`nav-links ${mobileMenu ? 'nav-open' : ''}`}>
          <a href="#home" onClick={() => setMobileMenu(false)}>{t.home}</a>
          <a href="#how-it-works" onClick={() => setMobileMenu(false)}>{t.how}</a>
          <a href="#about" onClick={() => setMobileMenu(false)}>{t.about}</a>
          <a href="#help" onClick={() => setMobileMenu(false)}>{t.help}</a>
          <div className="mobile-actions">
            {isAuthenticated ? (
              <button className="btn-primary" onClick={onDashboard}>Dashboard</button>
            ) : (
              <>
                <button className="btn-login" onClick={onLogin}>{t.login}</button>
                <button className="btn-primary" onClick={onSignup}>{t.signup}</button>
              </>
            )}
          </div>
        </nav>

        <div className="nav-actions">
          <button
            className="language-button"
            onClick={() => setLanguage(language === 'en' ? 'fr' : 'en')}
            aria-label="Change language"
          >
            <Globe2 size={17} />
            {language.toUpperCase()}
          </button>
          {isAuthenticated ? (
            <button className="btn-primary desktop-action" onClick={onDashboard}>Dashboard</button>
          ) : (
            <>
              <button className="btn-login desktop-action" onClick={onLogin}>{t.login}</button>
              <button className="btn-primary desktop-action" onClick={onSignup}>{t.signup}</button>
            </>
          )}
          <button
            className="menu-button"
            onClick={() => setMobileMenu(!mobileMenu)}
            aria-label="Toggle menu"
          >
            {mobileMenu ? <X /> : <Menu />}
          </button>
        </div>
      </header>

      <main>
        <section className="hero-section" id="home">
          <div className="hero-background-circle circle-one" />
          <div className="hero-background-circle circle-two" />

          <div className="hero-content">
            <div className="hero-copy">
              <div className="eyebrow">
                <span className="eyebrow-dot" />
                {t.badge}
              </div>

              <h1>
                {t.title1}
                <br />
                <span>{t.title2}</span>
              </h1>

              <p className="hero-description">{t.description}</p>

              <div className="hero-highlights">
                <span><CheckCircle2 size={17} /> Verified drivers</span>
                <span><CheckCircle2 size={17} /> Affordable rides</span>
              </div>
            </div>

            <div className="hero-visual" aria-label="Travel illustration">
              <div className="visual-orbit orbit-one" />
              <div className="visual-orbit orbit-two" />
              <div className="visual-sun" />
              <div className="visual-road">
                <div className="road-line" />
              </div>
              <div className="floating-card card-location">
                <span className="floating-icon"><MapPin size={19} /></span>
                <span><strong>Douala</strong><small>Departure city</small></span>
              </div>
              <div className="floating-card card-destination">
                <span className="floating-icon green"><MapPin size={19} /></span>
                <span><strong>Yaoundé</strong><small>Your destination</small></span>
              </div>
              <div className="car-illustration">
                <CarFront size={120} strokeWidth={1.25} />
              </div>
              <div className="visual-caption">
                <ShieldCheck size={17} />
                Travel safely with Travo
              </div>
            </div>
          </div>

          <div className="search-panel">
            <div className="search-heading">
              <div>
                <h2>Where would you like to go?</h2>
                <p>Find a ride that fits your journey.</p>
              </div>
              <span className="search-label"><span /> Cameroon routes</span>
            </div>

            <div className="search-fields">
              <label className="search-field">
                <span className="field-icon"><MapPin size={19} /></span>
                <span className="field-content">
                  <span className="field-label">{t.from}</span>
                  <select value={origin} onChange={(e) => setOrigin(e.target.value)}>
                    {cities.map((city) => <option key={city} value={city}>{city}</option>)}
                  </select>
                </span>
              </label>

              <button className="swap-button" onClick={swapLocations} aria-label="Swap locations">
                <ArrowLeftRight size={19} />
              </button>

              <label className="search-field">
                <span className="field-icon destination-icon"><MapPin size={19} /></span>
                <span className="field-content">
                  <span className="field-label">{t.to}</span>
                  <select value={destination} onChange={(e) => setDestination(e.target.value)}>
                    {cities.map((city) => <option key={city} value={city}>{city}</option>)}
                  </select>
                </span>
              </label>

              <label className="search-field date-field">
                <span className="field-icon"><CalendarDays size={19} /></span>
                <span className="field-content">
                  <span className="field-label">{t.date}</span>
                  <input
                    type="date"
                    min={today}
                    value={date}
                    onChange={(e) => setDate(e.target.value)}
                  />
                </span>
              </label>

              <label className="search-field passenger-field">
                <span className="field-icon"><Users size={19} /></span>
                <span className="field-content">
                  <span className="field-label">{t.passengers}</span>
                  <select
                    value={passengers}
                    onChange={(e) => setPassengers(Number(e.target.value))}
                  >
                    {[1, 2, 3, 4, 5, 6].map((number) => (
                      <option key={number} value={number}>
                        {number} {number === 1 ? 'person' : 'people'}
                      </option>
                    ))}
                  </select>
                </span>
              </label>

              <button className="search-button" onClick={() => void handleSearch()} disabled={searching}>
                <Search size={19} />
                {searching ? 'Searching…' : t.search}
              </button>
            </div>

            {notice && (
              <div className="search-notice" role="status">
                <CheckCircle2 size={17} />
                <span>{notice}</span>
                <button onClick={() => setNotice('')} aria-label="Dismiss notice"><X size={16} /></button>
              </div>
            )}
            {searchError && <p className="form-error" role="alert">{searchError}</p>}
            {hasSearched && (
              availableTrips.length ? (
                <div className="home-search-results" aria-live="polite">
                  {availableTrips.map((trip) => (
                    <article className="home-trip-result" key={trip.id}>
                      <div>
                        <strong>{trip.origin} → {trip.destination}</strong>
                        <p>{new Date(trip.departure_at).toLocaleString()} · {trip.available_seats} seats · {trip.driver_name}</p>
                      </div>
                      <div className="home-trip-action">
                        <span>{trip.price_per_seat.toLocaleString()} XAF / seat</span>
                        <button
                          className="platform-primary-button"
                          onClick={() => {
                            if (isAuthenticated && userRole !== 'passenger') {
                              setNotice('Use a passenger account to book this ride.')
                              return
                            }
                            onBookTrip(trip.id)
                          }}
                        >
                          Book
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              ) : (
                !searchError && <p className="empty-search-results">No available trips matched this route and date.</p>
              )
            )}
          </div>
        </section>

        <section className="popular-section section-container">
          <div className="section-heading">
            <div>
              <span className="section-kicker">EXPLORE CAMEROON</span>
              <h2>{t.popular}</h2>
              <p>{t.popularDescription}</p>
            </div>
            <a href="#home" className="text-link">Explore routes <ArrowRight size={17} /></a>
          </div>

          <div className="route-grid">
            {routes.map((route) => (
              <button
                className="route-card"
                key={`${route.from}-${route.to}`}
                onClick={() => {
                  setOrigin(route.from)
                  setDestination(route.to)
                  document.getElementById('home')?.scrollIntoView({ behavior: 'smooth' })
                  setNotice(`${route.from} → ${route.to}. Select your date and find a ride.`)
                }}
              >
                <div className="route-top">
                  <span className="route-icon"><CarFront size={20} /></span>
                  <span className="route-time">{route.time}</span>
                </div>
                <div className="route-cities">
                  <strong>{route.from}</strong>
                  <span className="route-line"><span /></span>
                  <strong>{route.to}</strong>
                </div>
                <div className="route-bottom">
                  <span>{route.price}</span>
                  <ArrowRight size={18} />
                </div>
              </button>
            ))}
          </div>
        </section>

        <section className="safety-section" id="about">
          <div className="section-container safety-layout">
            <div className="safety-copy">
              <span className="section-kicker">YOUR JOURNEY MATTERS</span>
              <h2>{t.safety}</h2>
              <p>{t.safetyDescription}</p>
              <a href="#help" className="text-link">Learn about Travo <ArrowRight size={17} /></a>
            </div>

            <div className="safety-features">
              <div className="feature-item">
                <span className="feature-icon"><ShieldCheck size={23} /></span>
                <div><h3>{t.verified}</h3><p>{t.verifiedText}</p></div>
              </div>
              <div className="feature-item">
                <span className="feature-icon"><MapPin size={23} /></span>
                <div><h3>{t.safe}</h3><p>{t.safeText}</p></div>
              </div>
              <div className="feature-item">
                <span className="feature-icon"><CheckCircle2 size={23} /></span>
                <div><h3>{t.support}</h3><p>{t.supportText}</p></div>
              </div>
            </div>
          </div>
        </section>

        <section className="how-section section-container" id="how-it-works">
          <div className="section-heading centered-heading">
            <div>
              <span className="section-kicker">SIMPLE. CONVENIENT. CONNECTED.</span>
              <h2>{t.howTitle}</h2>
            </div>
          </div>

          <div className="steps-grid">
            <div className="step-card">
              <span className="step-number">01</span>
              <span className="step-icon"><Search size={26} /></span>
              <h3>{t.step1}</h3>
              <p>{t.step1Text}</p>
            </div>
            <div className="step-card">
              <span className="step-number">02</span>
              <span className="step-icon"><CalendarDays size={26} /></span>
              <h3>{t.step2}</h3>
              <p>{t.step2Text}</p>
            </div>
            <div className="step-card">
              <span className="step-number">03</span>
              <span className="step-icon"><CarFront size={26} /></span>
              <h3>{t.step3}</h3>
              <p>{t.step3Text}</p>
            </div>
          </div>
        </section>
      </main>

      <footer className="footer" id="help">
        <div className="footer-inner">
          <a className="brand footer-brand" href="#home">
            <span className="brand-icon"><CarFront size={24} /></span>
            <span>travo<span className="brand-dot">.</span></span>
          </a>
          <p>{t.footer}</p>
          <span className="copyright">© {new Date().getFullYear()} Travo. All rights reserved.</span>
        </div>
      </footer>
    </div>
  )
}

export default App
