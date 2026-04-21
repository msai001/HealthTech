import axios from 'axios';

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api';

const apiClient = axios.create({
  baseURL: API_BASE_URL,
  withCredentials: true, // Для отправки куков
});

// Обработка ошибок
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', error);
    return Promise.reject(error);
  }
);

// Auth API
export const authAPI = {
  // Получить URL для Google OAuth
  getGoogleAuthUrl: () => {
    const clientId = import.meta.env.VITE_GOOGLE_CLIENT_ID;
    const redirectUrl = `${window.location.origin}/callback`;
    return `https://accounts.google.com/o/oauth2/v2/auth?client_id=${clientId}&redirect_uri=${redirectUrl}&response_type=code&scope=openid%20email%20profile`;
  },

  // Обработка callback
  handleCallback: (code) => apiClient.post('/auth/callback', { code }),

  // Проверить OTP код
  verifyOTP: (code) => apiClient.post('/auth/verify-otp', { code }),

  // Получить текущего пользователя
  getCurrentUser: () => apiClient.get('/auth/me'),

  // Выход
  logout: () => apiClient.post('/auth/logout'),
};

// Appointments API
export const appointmentsAPI = {
  // Получить все записи пользователя
  getAppointments: () => apiClient.get('/appointments'),

  // Создать новую запись
  createAppointment: (data) =>
    apiClient.post('/appointments', {
      doctor_name: data.doctor,
      appointment_date: data.date,
    }),

  // Удалить запись
  deleteAppointment: (id) => apiClient.delete(`/appointments/${id}`),
};

export default apiClient;
import axios from 'axios'

const API_BASE_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080'

const api = axios.create({
  baseURL: API_BASE_URL,
  withCredentials: true,
})

export const authService = {
  getGoogleAuthUrl: () => {
    return `${API_BASE_URL}/`
  },
  verifyOTP: (code) => api.post('/otp-check', { code }, {
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  }),
}

export const appointmentService = {
  getAppointments: () => api.get('/api/appointments'),
  addAppointment: (doctor, date) =>
    api.post('/add', { doc: doctor, date }, {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    }),
  deleteAppointment: (id) =>
    api.delete(`/delete?id=${id}`),
}

export default api
