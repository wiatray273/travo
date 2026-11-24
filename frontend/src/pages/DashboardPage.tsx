import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { CarFront, LogOut, ShieldCheck } from 'lucide-react'
import { ApiError, download, patch, post, remove, request, type Country, type User } from '../lib/api'
import './Platform.css'

interface DashboardPageProps {
  user: User
  onSignOut: () => void
  onUserUpdate: (user: User) => void
}

interface Trip {
  id: number
  origin: string
  destination: string
  departure_at: string
  seats: number
  available_seats: number
  price_per_seat: number
  status: string
  driver_name: string
}

interface Booking {
  id: number
  trip_id: number
  driver_id: number
  driver_phone?: string | null
  passenger_name?: string
  driver_name: string
  origin: string
  destination: string
  departure_at: string
  seats: number
  price_per_seat: number
  fare_total: number
  commission_amount: number
  provider_fee: number
  amount_charged: number
  provider_revenue: number
  driver_amount_due: number
  payment_status: string
  status: string
  payment_url?: string
}

interface DashboardSummary {
  users: number
  drivers: number
  passengers: number
  trips: number
  bookings: number
  commission_xaf: number
}

interface AdminUser {
  id: number
  full_name: string
  email?: string | null
  phone?: string | null
  role: string
  status: string
  country_id?: number | null
}

interface IdentityDocument {
  id: number
  document_type: string
  original_name: string
  back_original_name: string
  status: string
  rejection_reason?: string
}

interface DriverVerification {
  verification_status: string
  documents: IdentityDocument[]
}

interface DriverReview {
  user_id: number
  full_name: string
  email?: string | null
  phone?: string | null
  account_status: string
  verification_status: string
  documents: IdentityDocument[]
  photo: DriverPhoto | null
}

interface DriverPhoto {
  id: number
  review_status: string
  review_document_id?: number
}

interface PhotoReviewTarget {
  driverID: number
  driverName: string
  photoID: number
  documentID: number
  photoURL: string
  documentURL: string
}

interface PaymentLinkResponse {
  payment_url: string
  amount_charged: number
  fare: number
  estimated_mobile_money_fee: number
  commission: number
}

const xaf = (amount: number) => `${amount.toLocaleString()} XAF`

function documentRequirement(documents: IdentityDocument[], type: string) {
  const matching = documents.filter((document) => document.document_type === type)
  if (matching.some((document) => document.status === 'approved' && document.back_original_name)) return 'approved'
  if (matching.some((document) => document.status === 'pending' && document.back_original_name)) return 'awaiting review'
  if (matching.length > 0) return 'requires a new submission'
  return 'not submitted'
}

export default function DashboardPage({ user, onSignOut, onUserUpdate }: DashboardPageProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const processedTripID = useRef<number | null>(null)
  const [trips, setTrips] = useState<Trip[]>([])
  const [myTrips, setMyTrips] = useState<Trip[]>([])
  const [bookings, setBookings] = useState<Booking[]>([])
  const [users, setUsers] = useState<AdminUser[]>([])
  const [drivers, setDrivers] = useState<DriverReview[]>([])
  const [countries, setCountries] = useState<Country[]>([])
  const [summary, setSummary] = useState<DashboardSummary | null>(null)
  const [selectedTrip, setSelectedTrip] = useState<Trip | null>(null)
  const [seats, setSeats] = useState(1)
  const [origin, setOrigin] = useState('')
  const [destination, setDestination] = useState('')
  const [travelDate, setTravelDate] = useState('')
  const [departureAt, setDepartureAt] = useState('')
  const [tripOrigin, setTripOrigin] = useState('')
  const [tripDestination, setTripDestination] = useState('')
  const [tripSeats, setTripSeats] = useState(3)
  const [tripPrice, setTripPrice] = useState('')
  const [documentType, setDocumentType] = useState('national_id')
  const [documentFront, setDocumentFront] = useState<File | null>(null)
  const [documentBack, setDocumentBack] = useState<File | null>(null)
  const [uploadingDocument, setUploadingDocument] = useState(false)
  const [driverVerification, setDriverVerification] = useState<DriverVerification | null>(null)
  const [driverPhoto, setDriverPhoto] = useState<DriverPhoto | null>(null)
  const [photoFile, setPhotoFile] = useState<File | null>(null)
  const [photoConsent, setPhotoConsent] = useState(false)
  const [uploadingPhoto, setUploadingPhoto] = useState(false)
  const [photoReview, setPhotoReview] = useState<PhotoReviewTarget | null>(null)
  const [adminName, setAdminName] = useState('')
  const [adminEmail, setAdminEmail] = useState('')
  const [adminPassword, setAdminPassword] = useState('')
  const [adminCountryID, setAdminCountryID] = useState('')
  const [countryName, setCountryName] = useState('')
  const [countryISO2, setCountryISO2] = useState('')
  const [countryISO3, setCountryISO3] = useState('')
  const [countryCurrency, setCountryCurrency] = useState('XAF')
  const [countryPhoneCode, setCountryPhoneCode] = useState('')
  const [previewURL, setPreviewURL] = useState('')
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const [reviewError, setReviewError] = useState('')
  const [accountActionError, setAccountActionError] = useState<{ id: number; message: string } | null>(null)
  const [updatingAccountID, setUpdatingAccountID] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)

  const loadData = useCallback(async () => {
    try {
      if (user.role === 'passenger') {
        const [tripResult, bookingResult] = await Promise.all([
          request<{ trips: Trip[] }>('/trips'),
          request<{ bookings: Booking[] }>('/bookings'),
        ])
        setTrips(tripResult.trips)
        setBookings(bookingResult.bookings)
      } else if (user.role === 'driver') {
        const [tripResult, bookingResult, photoResult, verificationResult] = await Promise.all([
          request<{ trips: Trip[] }>('/trips/mine'),
          request<{ bookings: Booking[] }>('/bookings'),
          request<{ photo: DriverPhoto | null }>('/driver/photo'),
          request<DriverVerification>('/driver/verification'),
        ])
        setMyTrips(tripResult.trips)
        setBookings(bookingResult.bookings)
        setDriverPhoto(photoResult.photo)
        setDriverVerification(verificationResult)
      } else {
        const requests: [
          Promise<{ overview: DashboardSummary } | DashboardSummary>,
          Promise<{ users: AdminUser[] }>,
          Promise<{ drivers: DriverReview[] }>,
        ] = [
          request<DashboardSummary>('/admin/overview').then((overview) => ({ overview })),
          request<{ users: AdminUser[] }>('/admin/users'),
          request<{ drivers: DriverReview[] }>('/admin/drivers'),
        ]
        const [overviewResult, userResult, driverResult] = await Promise.all(requests)
        setSummary('overview' in overviewResult ? overviewResult.overview : overviewResult)
        setUsers(userResult.users)
        setDrivers(driverResult.drivers)
        if (user.role === 'super_admin') {
          const countryResult = await request<{ countries: Country[] }>('/admin/countries')
          setCountries(countryResult.countries)
          if (!adminCountryID && countryResult.countries[0]) {
            setAdminCountryID(String(countryResult.countries[0].id))
          }
        }
      }

    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not load your platform.')
    }
  }, [adminCountryID, user.role])

  useEffect(() => {
    const state = location.state as { bookTripId?: number } | null
    const tripID = state?.bookTripId
    if (!tripID) {
      processedTripID.current = null
      return
    }
    if (user.role !== 'passenger' || processedTripID.current === tripID) return

    processedTripID.current = tripID
    navigate(location.pathname, { replace: true, state: null })
    request<{ trip: Trip }>(`/trips/${tripID}`)
      .then(({ trip }) => setSelectedTrip(trip))
      .catch((cause: Error) => setError(cause.message))
  }, [location.pathname, location.state, navigate, user.role])

  useEffect(() => {
    let active = true
    loadData().finally(() => {
      if (active) setLoading(false)
    })
    return () => {
      active = false
    }
  }, [loadData])

  useEffect(() => {
    if (user.role !== 'driver' || loading || uploadingPhoto ||
        (user.status !== 'pending' && (!driverPhoto || driverPhoto.review_status === 'appears_match'))) return

    let active = true
    let timer: ReturnType<typeof setTimeout>
    const checkReview = async () => {
      try {
        const [account, photo, verification] = await Promise.all([
          request<{ user: User }>('/auth/me'),
          request<{ photo: DriverPhoto | null }>('/driver/photo'),
          request<DriverVerification>('/driver/verification'),
        ])
        if (!active) return
        setReviewError('')
        setDriverVerification(verification)
        if (account.user.status !== user.status) {
          onUserUpdate(account.user)
          if (account.user.status === 'active') setNotice('Your driver account is active.')
        }
        if (photo.photo?.id !== driverPhoto?.id || photo.photo?.review_status !== driverPhoto?.review_status) {
          setDriverPhoto(photo.photo)
          if (photo.photo?.id === driverPhoto?.id && photo.photo?.review_status === 'appears_match') {
            setNotice(account.user.status === 'active' && user.status !== 'active'
              ? 'Your photo was marked as a match and your driver account is active.'
              : 'Your photo was marked as a match.')
          }
        }
      } catch (cause) {
        if (active) setReviewError(cause instanceof Error ? `Could not refresh review status: ${cause.message}` : 'Could not refresh review status.')
      } finally {
        if (active) timer = setTimeout(checkReview, 10_000)
      }
    }
    timer = setTimeout(checkReview, 10_000)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [driverPhoto, loading, onUserUpdate, uploadingPhoto, user.role, user.status])

  useEffect(() => () => {
    if (photoReview) {
      URL.revokeObjectURL(photoReview.photoURL)
      URL.revokeObjectURL(photoReview.documentURL)
    }
  }, [photoReview])

  useEffect(() => () => {
    if (previewURL) URL.revokeObjectURL(previewURL)
  }, [previewURL])

  async function searchTrips(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const params = new URLSearchParams()
    if (origin.trim()) params.set('origin', origin.trim())
    if (destination.trim()) params.set('destination', destination.trim())
    if (travelDate) params.set('date', travelDate)
    try {
      const result = await request<{ trips: Trip[] }>(`/trips?${params.toString()}`)
      setTrips(result.trips)
      setError('')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not search trips.')
    }
  }

  async function bookTrip(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedTrip) return
    try {
      const result = await post<{ booking: Booking; message: string }>(
        `/trips/${selectedTrip.id}/bookings`,
        { seats },
      )
      setNotice(result.message)
      setSelectedTrip(null)
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not reserve seats.')
    }
  }

  async function startPayment(bookingID: number) {
    try {
      const payment = await post<PaymentLinkResponse>(`/bookings/${bookingID}/payment`)
      window.location.assign(payment.payment_url)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not start mobile-money checkout.')
    }
  }

  async function refreshPayment(bookingID: number) {
    try {
      await request(`/bookings/${bookingID}/payment-status`)
      setNotice('Payment status refreshed from Fapshi.')
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not verify payment.')
    }
  }

  async function createTrip(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await post('/trips', {
        origin: tripOrigin.trim(),
        destination: tripDestination.trim(),
        departure_at: new Date(departureAt).toISOString(),
        seats: tripSeats,
        price_per_seat: Number(tripPrice),
      })
      setNotice('Your trip is published.')
      setTripOrigin('')
      setTripDestination('')
      setDepartureAt('')
      setTripPrice('')
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not publish trip.')
    }
  }

  async function uploadDocument(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (uploadingDocument) return
    setError('')
    setNotice('')
    if (!documentFront || !documentBack) {
      setError('Choose both the front and back of the document.')
      return
    }
    if (documentFront.size > 5 * 1024 * 1024 || documentBack.size > 5 * 1024 * 1024) {
      setError('Each document side must be 5 MB or smaller.')
      return
    }
    const form = event.currentTarget
    const formData = new FormData()
    formData.set('document_type', documentType)
    formData.set('front', documentFront)
    formData.set('back', documentBack)
    setUploadingDocument(true)
    try {
      await request('/driver/documents', { method: 'POST', body: formData })
      const needsLicense = documentType === 'national_id' && driverVerification &&
        documentRequirement(driverVerification.documents, 'driver_license') === 'not submitted'
      setNotice(needsLicense
        ? 'National ID submitted. Next, upload the front and back of your driver’s license.'
        : 'Document submitted for administrator review.')
      setDocumentFront(null)
      setDocumentBack(null)
      form.reset()
      if (needsLicense) setDocumentType('driver_license')
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not upload document.')
    } finally {
      setUploadingDocument(false)
    }
  }

  async function uploadPhoto(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (uploadingPhoto) return
    setError('')
    setNotice('')
    if (!photoFile || !photoConsent) {
      setError('Choose a selfie and consent to its storage and sharing.')
      return
    }
    if (photoFile.size > 5 * 1024 * 1024) {
      setError('Photo must be 5 MB or smaller.')
      return
    }
    const form = event.currentTarget
    const data = new FormData()
    data.set('photo', photoFile)
    data.set('consent', 'true')
    setUploadingPhoto(true)
    try {
      const result = await request<{ photo: DriverPhoto }>('/driver/photo', { method: 'POST', body: data })
      setDriverPhoto(result.photo)
      setPhotoFile(null)
      setPhotoConsent(false)
      form.reset()
      setNotice('Profile photo saved for manual review.')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not upload photo.')
    } finally {
      setUploadingPhoto(false)
    }
  }

  async function saveDriverProfile(fullName: string, phone: string) {
    setError('')
    setNotice('')
    try {
      const result = await patch<{ user: User }>('/driver/profile', { full_name: fullName, phone })
      onUserUpdate(result.user)
      setNotice('Profile saved. Your phone number can also be used to log in.')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not save your profile.')
    }
  }

  async function updateTripStatus(trip: Trip) {
    try {
      await patch(`/trips/${trip.id}`, { status: trip.status === 'cancelled' ? 'published' : 'cancelled' })
      setNotice('Trip status updated.')
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not update trip.')
    }
  }

  async function updateUserStatus(account: AdminUser) {
    if (updatingAccountID !== null) return
    const status = account.status === 'active' ? 'suspended' : 'active'
    setAccountActionError(null)
    setUpdatingAccountID(account.id)
    try {
      await patch(`/admin/users/${account.id}/status`, { status })
      setNotice(`${account.full_name}'s account is ${status}.`)
      await loadData()
    } catch (cause) {
      setAccountActionError({
        id: account.id,
        message: cause instanceof Error ? cause.message : 'Could not update account.',
      })
    } finally {
      setUpdatingAccountID(null)
    }
  }

  async function reviewDocument(document: IdentityDocument, status: 'approved' | 'rejected') {
    try {
      await patch(`/admin/documents/${document.id}`, {
        status,
        rejection_reason: status === 'rejected' ? 'Please upload a valid document.' : '',
      })
      setNotice(`Document ${status}.`)
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not review document.')
    }
  }

  async function viewDocument(document: IdentityDocument, side: 'front' | 'back') {
    try {
      const blob = await download(`/admin/documents/${document.id}/file?side=${side}`)
      setPreviewURL(URL.createObjectURL(blob))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not open document.')
    }
  }

  async function compareDriverPhoto(driver: DriverReview, document: IdentityDocument) {
    if (!driver.photo) return
    setError('')
    try {
      const [photo, id] = await Promise.all([
        download(`/drivers/${driver.user_id}/photo`),
        download(`/admin/documents/${document.id}/file?side=front`),
      ])
      setPhotoReview({
        driverID: driver.user_id, driverName: driver.full_name, photoID: driver.photo.id,
        documentID: document.id, photoURL: URL.createObjectURL(photo), documentURL: URL.createObjectURL(id),
      })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not load comparison.')
    }
  }

  async function reviewPhoto(status: 'appears_match' | 'does_not_match' | 'unclear') {
    if (!photoReview) return
    try {
      await patch(`/admin/drivers/${photoReview.driverID}/photo-review`, {
        photo_id: photoReview.photoID, document_id: photoReview.documentID, status,
      })
      setNotice('Manual photo review saved. It does not verify liveness.')
      setPhotoReview(null)
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not save photo review.')
    }
  }

  async function createCountryAdmin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await post('/admin/country-admins', {
        full_name: adminName,
        email: adminEmail,
        password: adminPassword,
        country_id: Number(adminCountryID),
      })
      setNotice('Country administrator created.')
      setAdminName('')
      setAdminEmail('')
      setAdminPassword('')
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not create country administrator.')
    }
  }

  async function createCountry(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await post('/admin/countries', {
        name: countryName,
        iso2: countryISO2,
        iso3: countryISO3,
        currency_code: countryCurrency,
        phone_code: countryPhoneCode,
      })
      setNotice('Country added.')
      setCountryName('')
      setCountryISO2('')
      setCountryISO3('')
      setCountryPhoneCode('')
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not add country.')
    }
  }

  async function toggleCountry(country: Country) {
    try {
      await patch(`/admin/countries/${country.id}`, { is_active: !country.is_active })
      await loadData()
      setNotice(`${country.name} ${country.is_active ? 'deactivated' : 'activated'}.`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not update country.')
    }
  }

  async function cancelBooking(booking: Booking) {
    try {
      await remove(`/bookings/${booking.id}`)
      setNotice('Booking cancelled and seats released.')
      await loadData()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not cancel booking.')
    }
  }

  const title = user.role === 'passenger'
    ? 'Passenger platform'
    : user.role === 'driver'
      ? user.status === 'active' ? 'Driver dashboard' : 'Driver verification'
      : user.role === 'country_admin'
        ? 'Country admin platform'
        : 'Super admin platform'

  return (
    <main className="dashboard-page">
      <header className="dashboard-header">
        <Link to="/" className="brand" aria-label="Travo home">
          <span className="brand-icon"><CarFront size={22} /></span>
          <span>travo<span className="brand-dot">.</span></span>
        </Link>
        <div className="dashboard-user">
          <div><strong>{user.full_name}</strong>{user.role.replace('_', ' ')}</div>
          <button className="dashboard-logout" onClick={onSignOut}><LogOut size={14} /> Log out</button>
        </div>
      </header>

      <div className="dashboard-content">
        <div className="dashboard-title">
          <div>
            <span className="section-kicker">TRAVO WORKSPACE</span>
            <h1>{title}</h1>
            <p>Manage your travel activity and account securely.</p>
          </div>
          {user.status !== 'active' && <span className="dashboard-status">{user.status === 'pending' ? 'Account activation pending' : user.status}</span>}
        </div>

        {error && <p className="form-error" role="alert">{error}</p>}
        {reviewError && <p className="form-error" role="alert">{reviewError}</p>}
        {notice && <p className="inline-message" role="status">{notice}</p>}
        {loading && <div className="loading-state">Loading your workspace…</div>}

        {!loading && user.role === 'passenger' && (
          <PassengerPlatform
            trips={trips}
            bookings={bookings}
            selectedTrip={selectedTrip}
            seats={seats}
            setSeats={setSeats}
            setSelectedTrip={setSelectedTrip}
            origin={origin}
            destination={destination}
            travelDate={travelDate}
            setOrigin={setOrigin}
            setDestination={setDestination}
            setTravelDate={setTravelDate}
            onSearch={searchTrips}
            onBook={bookTrip}
            onPayment={startPayment}
            onContinuePayment={(paymentURL) => window.location.assign(paymentURL)}
            onRefreshPayment={refreshPayment}
            onCancel={cancelBooking}
          />
        )}

        {!loading && user.role === 'driver' && (
          <DriverPlatform
            user={user}
            trips={myTrips}
            bookings={bookings}
            tripOrigin={tripOrigin}
            tripDestination={tripDestination}
            departureAt={departureAt}
            tripSeats={tripSeats}
            tripPrice={tripPrice}
            setTripOrigin={setTripOrigin}
            setTripDestination={setTripDestination}
            setDepartureAt={setDepartureAt}
            setTripSeats={setTripSeats}
            setTripPrice={setTripPrice}
            onCreateTrip={createTrip}
            documentType={documentType}
            verification={driverVerification}
            setDocumentType={setDocumentType}
            setDocumentFront={setDocumentFront}
            setDocumentBack={setDocumentBack}
            uploadingDocument={uploadingDocument}
            onUploadDocument={uploadDocument}
            photo={driverPhoto}
            setPhotoFile={setPhotoFile}
            photoConsent={photoConsent}
            setPhotoConsent={setPhotoConsent}
            uploadingPhoto={uploadingPhoto}
            onUploadPhoto={uploadPhoto}
            onSaveProfile={saveDriverProfile}
            onUpdateTrip={updateTripStatus}
          />
        )}

        {!loading && (user.role === 'country_admin' || user.role === 'super_admin') && (
          <AdminPlatform
            user={user}
            summary={summary}
            users={users}
            drivers={drivers}
            countries={countries}
            adminName={adminName}
            adminEmail={adminEmail}
            adminPassword={adminPassword}
            adminCountryID={adminCountryID}
            setAdminName={setAdminName}
            setAdminEmail={setAdminEmail}
            setAdminPassword={setAdminPassword}
            setAdminCountryID={setAdminCountryID}
            countryName={countryName}
            countryISO2={countryISO2}
            countryISO3={countryISO3}
            countryCurrency={countryCurrency}
            countryPhoneCode={countryPhoneCode}
            setCountryName={setCountryName}
            setCountryISO2={setCountryISO2}
            setCountryISO3={setCountryISO3}
            setCountryCurrency={setCountryCurrency}
            setCountryPhoneCode={setCountryPhoneCode}
            previewURL={previewURL}
            accountActionError={accountActionError}
            updatingAccountID={updatingAccountID}
            onUpdateUser={updateUserStatus}
            onReviewDocument={reviewDocument}
            onViewDocument={viewDocument}
            photoReview={photoReview}
            onComparePhoto={compareDriverPhoto}
            onReviewPhoto={reviewPhoto}
            onClosePhotoReview={() => setPhotoReview(null)}
            onCreateCountryAdmin={createCountryAdmin}
            onCreateCountry={createCountry}
            onToggleCountry={toggleCountry}
          />
        )}
      </div>
    </main>
  )
}

function PrivateDriverPhoto({ driverID, label, photoVersion }: { driverID: number; label: string; photoVersion?: number }) {
  const [url, setURL] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    let objectURL = ''
    download(`/drivers/${driverID}/photo`)
      .then((blob) => {
        if (!active) return
        objectURL = URL.createObjectURL(blob)
        setURL(objectURL)
        setError('')
      })
      .catch((cause: Error) => {
        if (!active) return
        setURL('')
        setError(cause instanceof ApiError && cause.status === 404 ? 'No profile photo available.' : cause.message)
      })
    return () => {
      active = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [driverID, photoVersion])

  return url ? <img className="driver-profile-photo" src={url} alt={label} /> : error ? <p>{error}</p> : null
}

interface PassengerPlatformProps {
  trips: Trip[]
  bookings: Booking[]
  selectedTrip: Trip | null
  seats: number
  setSeats: (value: number) => void
  setSelectedTrip: (value: Trip | null) => void
  origin: string
  destination: string
  travelDate: string
  setOrigin: (value: string) => void
  setDestination: (value: string) => void
  setTravelDate: (value: string) => void
  onSearch: (event: FormEvent<HTMLFormElement>) => void
  onBook: (event: FormEvent<HTMLFormElement>) => void
  onPayment: (bookingID: number) => void
  onContinuePayment: (paymentURL: string) => void
  onRefreshPayment: (bookingID: number) => void
  onCancel: (booking: Booking) => void
}

function PassengerPlatform(props: PassengerPlatformProps) {
  const tripsSection = useRef<HTMLDetailsElement>(null)

  return (
    <>
      <section className="dashboard-card">
        <h2>Find a ride</h2>
        <form className="dashboard-form" onSubmit={(event) => {
          props.onSearch(event)
          if (tripsSection.current) tripsSection.current.open = true
        }}>
          <label>Leaving from<input className="platform-input" value={props.origin} onChange={(event) => props.setOrigin(event.target.value)} placeholder="Douala" /></label>
          <label>Going to<input className="platform-input" value={props.destination} onChange={(event) => props.setDestination(event.target.value)} placeholder="Yaoundé" /></label>
          <label>Travel date<input className="platform-input" type="date" value={props.travelDate} onChange={(event) => props.setTravelDate(event.target.value)} /></label>
          <button className="platform-primary-button">Search trips</button>
        </form>
      </section>

      {props.selectedTrip && (
        <section className="dashboard-card">
          <h2>Reserve seats: {props.selectedTrip.origin} → {props.selectedTrip.destination}</h2>
          <p>{new Date(props.selectedTrip.departure_at).toLocaleString()} · {xaf(props.selectedTrip.price_per_seat)} per seat · {props.selectedTrip.available_seats} seats remain</p>
          <form className="dashboard-form" onSubmit={props.onBook}>
            <label>Seats
              <select className="platform-input" value={props.seats} onChange={(event) => props.setSeats(Number(event.target.value))}>
                {Array.from({ length: Math.min(8, props.selectedTrip.available_seats) }, (_, index) => index + 1)
                  .map((count) => <option value={count} key={count}>{count}</option>)}
              </select>
            </label>
            <button className="platform-primary-button">Reserve seats</button>
            <button type="button" className="platform-secondary-button" onClick={() => props.setSelectedTrip(null)}>Close</button>
          </form>
        </section>
      )}

      <details className="dashboard-card collapsible-card" ref={tripsSection}>
        <summary><h2>Available trips ({props.trips.length})</h2></summary>
        <div className="platform-list">
          {props.trips.length === 0 && <p>No trips are currently available for this search.</p>}
          {props.trips.map((trip) => (
            <article className="platform-list-item" key={trip.id}>
              <div>
                <h3>{trip.origin} → {trip.destination}</h3>
                <p>{new Date(trip.departure_at).toLocaleString()} · {trip.available_seats} seats · Driver {trip.driver_name}<br />{xaf(trip.price_per_seat)} per seat</p>
              </div>
              <div className="platform-list-actions">
                <button className="action-primary" onClick={() => { props.setSelectedTrip(trip); props.setSeats(1) }}>Choose ride</button>
              </div>
            </article>
          ))}
        </div>
      </details>

      <details className="dashboard-card collapsible-card">
        <summary><h2>Your bookings ({props.bookings.length})</h2></summary>
        <div className="platform-list">
          {props.bookings.length === 0 && <p>You have not booked a ride yet.</p>}
          {props.bookings.map((booking) => (
            <article className="platform-list-item" key={booking.id}>
              <div>
                <h3>{booking.origin} → {booking.destination}</h3>
                {['pending_payment', 'confirmed'].includes(booking.status) && (
                  <PrivateDriverPhoto driverID={booking.driver_id} label={`${booking.driver_name}'s profile photo`} />
                )}
                <p>Driver: {booking.driver_name}</p>
                {booking.status === 'confirmed' && booking.payment_status === 'SUCCESSFUL' && (
                  <p>Driver phone: {booking.driver_phone
                    ? <a href={`tel:${booking.driver_phone}`}>{booking.driver_phone}</a>
                    : 'Not available; please contact support.'}</p>
                )}
                <p>{new Date(booking.departure_at).toLocaleString()} · {booking.seats} seat(s) · {xaf(booking.fare_total)} fare</p>
                <p>Mobile-money fee: {xaf(booking.provider_fee)} · Total: {xaf(booking.amount_charged)}</p>
                <span className={`status-pill status-${booking.status}`}>{booking.status.replace('_', ' ')}</span>
                {' '}
                <span className={`status-pill status-${booking.payment_status.toLowerCase()}`}>{booking.payment_status}</span>
              </div>
              <div className="platform-list-actions">
                {booking.status === 'pending_payment' && booking.payment_status === 'not_started' && (
                  <button className="action-primary" onClick={() => props.onPayment(booking.id)}>Pay with mobile money</button>
                )}
                {booking.status === 'pending_payment' && booking.payment_url && (
                  <button className="action-primary" onClick={() => props.onContinuePayment(booking.payment_url!)}>Continue checkout</button>
                )}
                {booking.status === 'pending_payment' && ['CREATED', 'PENDING'].includes(booking.payment_status) && (
                  <button onClick={() => props.onRefreshPayment(booking.id)}>Check payment</button>
                )}
                {booking.payment_status === 'unknown' && <p>Payment is uncertain. Contact support with booking ID {booking.id}; do not retry.</p>}
                {booking.status === 'pending_payment' && booking.payment_status === 'not_started' && (
                  <button className="action-danger" onClick={() => props.onCancel(booking)}>Cancel</button>
                )}
              </div>
            </article>
          ))}
        </div>
      </details>
    </>
  )
}

interface DriverPlatformProps {
  user: User
  trips: Trip[]
  bookings: Booking[]
  tripOrigin: string
  tripDestination: string
  departureAt: string
  tripSeats: number
  tripPrice: string
  setTripOrigin: (value: string) => void
  setTripDestination: (value: string) => void
  setDepartureAt: (value: string) => void
  setTripSeats: (value: number) => void
  setTripPrice: (value: string) => void
  onCreateTrip: (event: FormEvent<HTMLFormElement>) => void
  documentType: string
  verification: DriverVerification | null
  setDocumentType: (value: string) => void
  setDocumentFront: (value: File | null) => void
  setDocumentBack: (value: File | null) => void
  uploadingDocument: boolean
  onUploadDocument: (event: FormEvent<HTMLFormElement>) => void
  photo: DriverPhoto | null
  setPhotoFile: (value: File | null) => void
  photoConsent: boolean
  setPhotoConsent: (value: boolean) => void
  uploadingPhoto: boolean
  onUploadPhoto: (event: FormEvent<HTMLFormElement>) => void
  onSaveProfile: (fullName: string, phone: string) => Promise<void>
  onUpdateTrip: (trip: Trip) => void
}

function DriverPlatform(props: DriverPlatformProps) {
  const [section, setSection] = useState<'dashboard' | 'profile'>('dashboard')
  const [fullName, setFullName] = useState(props.user.full_name)
  const [phone, setPhone] = useState(props.user.phone ?? '')
  const [savingProfile, setSavingProfile] = useState(false)
  const showProfile = props.user.status !== 'active' || section === 'profile'
  const paidBookings = props.bookings.filter((booking) => booking.status === 'confirmed' && booking.payment_status === 'SUCCESSFUL')
  const amountDue = paidBookings.reduce((total, booking) => total + booking.driver_amount_due, 0)

  function selectDocument(type: string) {
    if (type === props.documentType) return
    props.setDocumentFront(null)
    props.setDocumentBack(null)
    props.setDocumentType(type)
  }

  async function saveProfile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (savingProfile) return
    setSavingProfile(true)
    try {
      await props.onSaveProfile(fullName.trim(), phone.trim())
    } finally {
      setSavingProfile(false)
    }
  }

  return (
    <>
      {props.user.status === 'active' && (
        <nav className="driver-menu" aria-label="Driver menu">
          <button type="button" className={section === 'dashboard' ? 'selected' : ''} aria-current={section === 'dashboard' ? 'page' : undefined} onClick={() => setSection('dashboard')}>Dashboard</button>
          <button type="button" className={section === 'profile' ? 'selected' : ''} aria-current={section === 'profile' ? 'page' : undefined} onClick={() => setSection('profile')}>Edit profile</button>
        </nav>
      )}
      {props.user.status !== 'active' && (
        <section className="dashboard-card form-note"><ShieldCheck size={18} />
          Submit the front and back of your national ID and driver’s license. After approving both documents, an administrator must activate your account. Your dashboard will then open automatically.
        </section>
      )}

      {showProfile ? <>
      <section className="dashboard-card">
        <h2>Verify your driver account</h2>
        <p>Both documents are required. Upload the <strong>front and back</strong> of each one separately. An administrator must approve both and activate your account.</p>
        {props.verification && <p>Verification status: <span className={`status-pill status-${props.verification.verification_status}`}>
          {props.verification.verification_status}
        </span></p>}
        {!props.verification && <p role="status">Verification status is unavailable. You can still submit documents.</p>}
        <div className="verification-steps" aria-label="Required documents">
          {[
            { type: 'national_id', name: 'National ID', number: 1 },
            { type: 'driver_license', name: 'Driver’s license', number: 2 },
          ].map(({ type, name, number }) => (
            <button type="button" className={`verification-step${props.documentType === type ? ' selected' : ''}`}
              key={type} aria-pressed={props.documentType === type} disabled={props.uploadingDocument}
              onClick={() => selectDocument(type)}>
              <span className="verification-step-number">{number}</span>
              <span><strong>{name}</strong><small>Front and back required</small>
                <span className="verification-step-status">{props.verification ? documentRequirement(props.verification.documents, type) : 'Status unavailable'}</span>
              </span>
            </button>
          ))}
        </div>
        <div className="verification-upload">
          <h3>Upload {props.documentType === 'national_id' ? 'national ID' : 'driver’s license'}</h3>
          <p>Choose a clear image or PDF for each side. Each file must be 5 MB or smaller.</p>
          <form key={props.documentType} className="dashboard-form" onSubmit={props.onUploadDocument}>
            <label>Front side
              <input className="platform-input" type="file" accept=".pdf,.jpg,.jpeg,.png,application/pdf,image/jpeg,image/png" onChange={(event) => props.setDocumentFront(event.target.files?.[0] ?? null)} disabled={props.uploadingDocument} required />
            </label>
            <label>Back side
              <input className="platform-input" type="file" accept=".pdf,.jpg,.jpeg,.png,application/pdf,image/jpeg,image/png" onChange={(event) => props.setDocumentBack(event.target.files?.[0] ?? null)} disabled={props.uploadingDocument} required />
            </label>
            <button className="platform-primary-button" disabled={props.uploadingDocument}>{props.uploadingDocument ? 'Uploading…' : `Submit ${props.documentType === 'national_id' ? 'national ID' : 'driver’s license'}`}</button>
          </form>
        </div>
        {props.verification && props.verification.documents.length > 0 ? (
          <details className="verification-history">
            <summary>View submitted documents ({props.verification.documents.length})</summary>
            <div className="platform-list">
            {props.verification.documents.map((document) => (
              <article className="platform-list-item" key={document.id}>
                <div>
                  <h3>{document.document_type === 'national_id' ? 'National ID' : 'Driver’s license'}</h3>
                  <p>{document.original_name}{document.back_original_name ? ` · ${document.back_original_name}` : ''}</p>
                  {document.rejection_reason && <p className="form-error">{document.rejection_reason}</p>}
                </div>
                <span className={`status-pill status-${document.status}`}>{document.status}</span>
              </article>
            ))}
            </div>
          </details>
        ) : null}
      </section>

      <section className="dashboard-card">
        <h2>Profile details</h2>
        <p>Your phone number is shared with passengers only after their booking is confirmed and paid. It can also be used to log in.</p>
        <form className="dashboard-form" onSubmit={saveProfile}>
          <label>Full name<input className="platform-input" value={fullName} onChange={(event) => setFullName(event.target.value)} minLength={2} maxLength={120} required /></label>
          <label>Phone number<input className="platform-input" type="tel" value={phone} onChange={(event) => setPhone(event.target.value)} maxLength={24} required /></label>
          <button className="platform-primary-button" disabled={savingProfile}>{savingProfile ? 'Saving…' : 'Save profile'}</button>
        </form>
      </section>

      <section className="dashboard-card">
        <h2>Profile photo and manual identity review</h2>
        {props.photo && (
          <>
            <PrivateDriverPhoto driverID={props.user.id} label="Your profile photo" photoVersion={props.photo.id} />
            <p>Manual photo review: {props.photo.review_status.replaceAll('_', ' ')}</p>
          </>
        )}
        <p>Upload a clear selfie. An administrator may compare it with your ID; no automated face matching or liveness check is performed.</p>
        <form className="dashboard-form" onSubmit={props.onUploadPhoto}>
          <label>Selfie (JPEG or PNG; max 5 MB and 25 megapixels)
            <input className="platform-input" type="file" accept="image/jpeg,image/png" capture="user" onChange={(event) => props.setPhotoFile(event.target.files?.[0] ?? null)} disabled={props.uploadingPhoto} required />
          </label>
          <label className="photo-consent">
            <input type="checkbox" checked={props.photoConsent} onChange={(event) => props.setPhotoConsent(event.target.checked)} disabled={props.uploadingPhoto} required />
            I agree that Travo retains this selfie and any previous selfies for security and shares the current photo with authorized administrators and passengers with an active booking on my trips.
          </label>
          <button className="platform-primary-button" disabled={props.uploadingPhoto}>{props.uploadingPhoto ? 'Uploading…' : 'Save profile photo'}</button>
        </form>
      </section>

      </> : <>
      <div className="dashboard-grid driver-summary">
        <div className="dashboard-card stat-card"><span>Your trips</span><strong>{props.trips.length}</strong></div>
        <div className="dashboard-card stat-card"><span>Paid bookings</span><strong>{paidBookings.length}</strong></div>
        <div className="dashboard-card stat-card"><span>Amount due from paid bookings</span><strong>{xaf(amountDue)}</strong><p>Recorded for reconciliation, not an automatic payout.</p></div>
      </div>
      <section className="dashboard-card">
        <h2>Publish a trip</h2>
        {!props.user.phone?.trim() ? (
          <p>Add your phone number in <button type="button" className="driver-inline-link" onClick={() => setSection('profile')}>Edit profile</button> before publishing a trip.</p>
        ) : (
          <form className="dashboard-form" onSubmit={props.onCreateTrip}>
            <label>Leaving from<input className="platform-input" value={props.tripOrigin} onChange={(event) => props.setTripOrigin(event.target.value)} required /></label>
            <label>Going to<input className="platform-input" value={props.tripDestination} onChange={(event) => props.setTripDestination(event.target.value)} required /></label>
            <label>Departure<input className="platform-input" type="datetime-local" value={props.departureAt} onChange={(event) => props.setDepartureAt(event.target.value)} required /></label>
            <label>Seats available<input className="platform-input" type="number" min={1} max={8} value={props.tripSeats} onChange={(event) => props.setTripSeats(Number(event.target.value))} required /></label>
            <label>Price per seat (XAF)<input className="platform-input" type="number" min={100} max={10000000} value={props.tripPrice} onChange={(event) => props.setTripPrice(event.target.value)} required /></label>
            <button className="platform-primary-button">Publish trip</button>
          </form>
        )}
      </section>

      <details className="dashboard-card collapsible-card" open>
        <summary><h2>Your trips ({props.trips.length})</h2></summary>
        <div className="platform-list">
          {props.trips.length === 0 && <p>You have not published trips.</p>}
          {props.trips.map((trip) => (
            <article className="platform-list-item" key={trip.id}>
              <div>
                <h3>{trip.origin} → {trip.destination}</h3>
                <p>{new Date(trip.departure_at).toLocaleString()} · {trip.available_seats}/{trip.seats} seats · {xaf(trip.price_per_seat)} per seat</p>
                <span className={`status-pill status-${trip.status}`}>{trip.status}</span>
              </div>
              <div className="platform-list-actions">
                {trip.status !== 'cancelled' && <button className="action-danger" onClick={() => props.onUpdateTrip(trip)}>Cancel trip</button>}
              </div>
            </article>
          ))}
        </div>
      </details>

      <details className="dashboard-card collapsible-card" open>
        <summary><h2>Passenger bookings ({props.bookings.length})</h2></summary>
        <div className="platform-list">
          {props.bookings.length === 0 && <p>No bookings for your trips yet.</p>}
          {props.bookings.map((booking) => (
            <article className="platform-list-item" key={booking.id}>
              <div>
                <h3>{booking.origin} → {booking.destination}</h3>
                <p>{booking.passenger_name} · {booking.seats} seat(s) · {new Date(booking.departure_at).toLocaleString()}</p>
                <span className={`status-pill status-${booking.status}`}>{booking.status.replace('_', ' ')}</span>
                {booking.status === 'confirmed' && booking.payment_status === 'SUCCESSFUL' && <p>Platform commission: {xaf(booking.commission_amount)} · Driver amount due: {xaf(booking.driver_amount_due)}</p>}
              </div>
            </article>
          ))}
        </div>
      </details>
      </>}
    </>
  )
}

interface AdminPlatformProps {
  user: User
  summary: DashboardSummary | null
  users: AdminUser[]
  drivers: DriverReview[]
  countries: Country[]
  adminName: string
  adminEmail: string
  adminPassword: string
  adminCountryID: string
  setAdminName: (value: string) => void
  setAdminEmail: (value: string) => void
  setAdminPassword: (value: string) => void
  setAdminCountryID: (value: string) => void
  countryName: string
  countryISO2: string
  countryISO3: string
  countryCurrency: string
  countryPhoneCode: string
  setCountryName: (value: string) => void
  setCountryISO2: (value: string) => void
  setCountryISO3: (value: string) => void
  setCountryCurrency: (value: string) => void
  setCountryPhoneCode: (value: string) => void
  previewURL: string
  accountActionError: { id: number; message: string } | null
  updatingAccountID: number | null
  onUpdateUser: (user: AdminUser) => void
  onReviewDocument: (document: IdentityDocument, status: 'approved' | 'rejected') => void
  onViewDocument: (document: IdentityDocument, side: 'front' | 'back') => void
  photoReview: PhotoReviewTarget | null
  onComparePhoto: (driver: DriverReview, document: IdentityDocument) => void
  onReviewPhoto: (status: 'appears_match' | 'does_not_match' | 'unclear') => void
  onClosePhotoReview: () => void
  onCreateCountryAdmin: (event: FormEvent<HTMLFormElement>) => void
  onCreateCountry: (event: FormEvent<HTMLFormElement>) => void
  onToggleCountry: (country: Country) => void
}

function AdminPlatform(props: AdminPlatformProps) {
  return (
    <>
      {props.summary && (
        <div className="dashboard-grid">
          {[
            ['Users', props.summary.users],
            ['Drivers', props.summary.drivers],
            ['Passengers', props.summary.passengers],
            ['Trips', props.summary.trips],
            ['Bookings', props.summary.bookings],
            ['Commission earned', xaf(props.summary.commission_xaf)],
          ].map(([label, value]) => (
            <div className="dashboard-card stat-card" key={label}>
              <span>{label}</span><strong>{value}</strong>
            </div>
          ))}
        </div>
      )}

      {props.user.role === 'super_admin' && (
        <>
          <section className="dashboard-card">
            <h2>Create a country administrator</h2>
            <form className="dashboard-form" onSubmit={props.onCreateCountryAdmin}>
              <label>Full name<input className="platform-input" value={props.adminName} onChange={(event) => props.setAdminName(event.target.value)} required minLength={2} /></label>
              <label>Email<input className="platform-input" type="email" value={props.adminEmail} onChange={(event) => props.setAdminEmail(event.target.value)} required /></label>
              <label>Password<input className="platform-input" type="password" value={props.adminPassword} onChange={(event) => props.setAdminPassword(event.target.value)} required minLength={8} maxLength={72} /></label>
              <label>Country<select className="platform-input" value={props.adminCountryID} onChange={(event) => props.setAdminCountryID(event.target.value)} required>
                {props.countries.filter((country) => country.is_active).map((country) => <option value={country.id} key={country.id}>{country.name}</option>)}
              </select></label>
              <button className="platform-primary-button">Create administrator</button>
            </form>
          </section>
          <section className="dashboard-card">
            <h2>Add a supported country</h2>
            <form className="dashboard-form" onSubmit={props.onCreateCountry}>
              <label>Country name<input className="platform-input" value={props.countryName} onChange={(event) => props.setCountryName(event.target.value)} required /></label>
              <label>ISO-2<input className="platform-input" value={props.countryISO2} onChange={(event) => props.setCountryISO2(event.target.value.toUpperCase())} maxLength={2} required /></label>
              <label>ISO-3<input className="platform-input" value={props.countryISO3} onChange={(event) => props.setCountryISO3(event.target.value.toUpperCase())} maxLength={3} /></label>
              <label>Currency code<input className="platform-input" value={props.countryCurrency} onChange={(event) => props.setCountryCurrency(event.target.value.toUpperCase())} maxLength={3} /></label>
              <label>Phone code<input className="platform-input" value={props.countryPhoneCode} onChange={(event) => props.setCountryPhoneCode(event.target.value)} /></label>
              <button className="platform-primary-button">Add country</button>
            </form>
          </section>
          <details className="dashboard-card collapsible-card">
            <summary><h2>Countries ({props.countries.length})</h2></summary>
            <div className="platform-list">
              {props.countries.map((country) => (
                <article className="platform-list-item" key={country.id}>
                  <div><h3>{country.name} ({country.iso2})</h3><p>{country.currency_code} · {country.phone_code}</p></div>
                  <div className="platform-list-actions">
                    <span className={`status-pill status-${country.is_active ? 'active' : 'suspended'}`}>{country.is_active ? 'active' : 'inactive'}</span>
                    <button onClick={() => props.onToggleCountry(country)}>{country.is_active ? 'Deactivate' : 'Activate'}</button>
                  </div>
                </article>
              ))}
            </div>
          </details>
        </>
      )}

      <details className="dashboard-card collapsible-card">
        <summary><h2>Driver verification ({props.drivers.length})</h2></summary>
        <div className="platform-list">
          {props.drivers.length === 0 && <p>No driver accounts in your country.</p>}
          {props.drivers.map((driver) => (
            <article className="platform-list-item" key={driver.user_id}>
              <div>
                <h3>{driver.full_name}</h3>
                <p>{driver.email || driver.phone} · Account: {driver.account_status} · Verification: {driver.verification_status}</p>
                {driver.verification_status !== 'approved' && (
                  <p>National ID: {documentRequirement(driver.documents, 'national_id')} · Driver’s license: {documentRequirement(driver.documents, 'driver_license')}</p>
                )}
                <p>Manual photo review: {driver.photo ? driver.photo.review_status.replaceAll('_', ' ') : 'no selfie uploaded'}
                  {driver.photo?.review_document_id ? ` · ID #${driver.photo.review_document_id}` : ''}</p>
                <div className="platform-list">
                  {driver.documents.map((document) => (
                    <div key={document.id}>
                      <p>{document.document_type.replace('_', ' ')} · Front: {document.original_name} · Back: {document.back_original_name || 'not uploaded'} · <span className={`status-pill status-${document.status}`}>{document.status}</span></p>
                      <div className="platform-list-actions">
                        <button onClick={() => props.onViewDocument(document, 'front')}>View front</button>
                        {driver.photo && document.document_type === 'national_id' && (
                          <button onClick={() => props.onComparePhoto(driver, document)}>Compare selfie and ID</button>
                        )}
                        {document.back_original_name && <button onClick={() => props.onViewDocument(document, 'back')}>View back</button>}
                        {document.status === 'pending' && <>
                          {document.back_original_name && <button className="action-primary" onClick={() => props.onReviewDocument(document, 'approved')}>Approve</button>}
                          <button className="action-danger" onClick={() => props.onReviewDocument(document, 'rejected')}>Reject</button>
                        </>}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
              <div className="platform-list-actions">
                {driver.account_status !== 'active' && driver.verification_status === 'approved' && (
                  <button className="action-primary" disabled={props.updatingAccountID !== null} onClick={() => props.onUpdateUser({
                    id: driver.user_id, full_name: driver.full_name, email: driver.email,
                    phone: driver.phone, role: 'driver', status: driver.account_status,
                  })}>{props.updatingAccountID === driver.user_id ? 'Updating…' : 'Activate driver'}</button>
                )}
                {props.accountActionError?.id === driver.user_id && <p className="form-error" role="alert">{props.accountActionError.message}</p>}
              </div>
            </article>
          ))}
        </div>
        {props.previewURL && <iframe className="document-preview" title="Identity document preview" src={props.previewURL} />}
        {props.photoReview && (
          <div className="photo-review">
            <h3>Manual comparison: {props.photoReview.driverName}</h3>
            <p>Compare the selfie with national ID #{props.photoReview.documentID}. This does not verify liveness.</p>
            <div className="photo-comparison">
              <div><h4>Selfie</h4><img src={props.photoReview.photoURL} alt="Driver selfie for manual review" /></div>
              <div><h4>ID front</h4><iframe className="document-preview" title="National ID front for manual review" src={props.photoReview.documentURL} /></div>
            </div>
            <div className="platform-list-actions">
              <button onClick={() => props.onReviewPhoto('appears_match')}>Appears to match</button>
              <button onClick={() => props.onReviewPhoto('does_not_match')}>Does not appear to match</button>
              <button onClick={() => props.onReviewPhoto('unclear')}>Unclear</button>
              <button onClick={props.onClosePhotoReview}>Close</button>
            </div>
          </div>
        )}
      </details>

      <details className="dashboard-card collapsible-card">
        <summary><h2>Manage accounts ({props.users.length})</h2></summary>
        <div className="platform-list">
          {props.users.map((account) => (
            <article className="platform-list-item" key={account.id}>
              <div>
                <h3>{account.full_name}</h3>
                <p>{account.email || account.phone} · {account.role.replace('_', ' ')} · <span className={`status-pill status-${account.status}`}>{account.status}</span></p>
              </div>
              {account.id !== props.user.id &&
                (['passenger', 'driver'].includes(account.role) ||
                  props.user.role === 'super_admin' && account.role === 'country_admin') && (
                <div className="platform-list-actions">
                  <button disabled={props.updatingAccountID !== null} className={account.status === 'active' ? 'action-danger' : 'action-primary'} onClick={() => props.onUpdateUser(account)}>
                    {props.updatingAccountID === account.id ? 'Updating…' : account.status === 'active' ? 'Suspend' : 'Activate'}
                  </button>
                  {account.role === 'driver' && account.status !== 'active' &&
                    props.drivers.some((driver) => driver.user_id === account.id && driver.verification_status !== 'approved') && (
                      <p>Approve both identity documents in Driver verification before activating.</p>
                    )}
                  {props.accountActionError?.id === account.id && <p className="form-error" role="alert">{props.accountActionError.message}</p>}
                </div>
              )}
            </article>
          ))}
        </div>
      </details>
    </>
  )
}
