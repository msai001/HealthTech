import { create } from 'zustand';

const useAuthStore = create((set) => ({
  user: null,
  isAuthenticated: false,
  loading: false,
  error: null,

  // Устанавливаем пользователя после авторизации
  setUser: (email) => set({ user: { email }, isAuthenticated: true }),

  // Очищаем данные пользователя при выходе
  logout: () => set({ user: null, isAuthenticated: false }),

  // Устанавливаем состояние загрузки
  setLoading: (loading) => set({ loading }),

  // Устанавливаем ошибку
  setError: (error) => set({ error }),

  // Очищаем ошибку
  clearError: () => set({ error: null }),
}));

export default useAuthStore;
