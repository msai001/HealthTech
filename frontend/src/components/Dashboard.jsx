import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { appointmentsAPI, authAPI } from '../services/api';
import useAuthStore from '../store/authStore';
import useAppointmentStore from '../store/appointmentStore';
import AppointmentForm from './AppointmentForm';
import AppointmentList from './AppointmentList';

export default function Dashboard() {
  const navigate = useNavigate();
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);
  const appointments = useAppointmentStore((state) => state.appointments);
  const setAppointments = useAppointmentStore(
    (state) => state.setAppointments
  );
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    if (!user) {
      navigate('/');
      return;
    }

    const fetchAppointments = async () => {
      try {
        setLoading(true);
        const response = await appointmentsAPI.getAppointments();
        setAppointments(response.data?.appointments || []);
      } catch (err) {
        console.error('Error fetching appointments:', err);
        setError('Ошибка при загрузке записей');
      } finally {
        setLoading(false);
      }
    };

    fetchAppointments();
  }, [user, navigate, setAppointments]);

  const handleLogout = async () => {
    try {
      await authAPI.logout();
    } catch (err) {
      console.error('Logout error:', err);
    } finally {
      logout();
      navigate('/');
    }
  };

  return (
    <div className="min-h-screen bg-slate-50 dark:bg-slate-900">
      {/* Header */}
      <header className="bg-white dark:bg-slate-800 border-b border-slate-200 dark:border-slate-700">
        <div className="max-w-4xl mx-auto px-4 py-6 flex justify-between items-center">
          <div>
            <h1 className="text-2xl font-bold text-slate-900 dark:text-white">
              🌿 HealthTech
            </h1>
            <p className="text-slate-500 dark:text-slate-400 text-sm">
              {user?.email}
            </p>
          </div>
          <button
            onClick={handleLogout}
            className="btn-secondary"
          >
            Выход
          </button>
        </div>
      </header>

      {/* Main Content */}
      <main className="max-w-4xl mx-auto px-4 py-8">
        {error && (
          <div className="bg-red-100 dark:bg-red-900 border border-red-400 dark:border-red-700 text-red-700 dark:text-red-200 px-4 py-3 rounded-lg mb-6">
            {error}
          </div>
        )}

        <div className="grid md:grid-cols-3 gap-6">
          {/* Form */}
          <div className="md:col-span-1">
            <AppointmentForm />
          </div>

          {/* List */}
          <div className="md:col-span-2">
            <div className="card">
              <h2 className="text-xl font-bold text-slate-900 dark:text-white mb-4">
                Мои записи
              </h2>

              {loading ? (
                <div className="flex justify-center py-8">
                  <div className="animate-spin h-8 w-8 border-4 border-emerald-500 border-t-transparent rounded-full"></div>
                </div>
              ) : appointments.length === 0 ? (
                <p className="text-center text-slate-500 dark:text-slate-400 py-8">
                  У вас нет записей. Создайте новую запись слева.
                </p>
              ) : (
                <AppointmentList appointments={appointments} />
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
