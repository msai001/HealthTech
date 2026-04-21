import { create } from 'zustand';

const useAppointmentStore = create((set) => ({
  appointments: [],
  loading: false,
  error: null,

  // Устанавливаем список записей
  setAppointments: (appointments) => set({ appointments }),

  // Добавляем новую запись
  addAppointment: (appointment) =>
    set((state) => ({
      appointments: [...state.appointments, appointment],
    })),

  // Удаляем запись
  removeAppointment: (id) =>
    set((state) => ({
      appointments: state.appointments.filter((app) => app.id !== id),
    })),

  // Устанавливаем состояние загрузки
  setLoading: (loading) => set({ loading }),

  // Устанавливаем ошибку
  setError: (error) => set({ error }),

  // Очищаем ошибку
  clearError: () => set({ error: null }),
}));

export default useAppointmentStore;
