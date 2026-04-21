import { useState } from 'react';
import { appointmentsAPI } from '../services/api';
import useAppointmentStore from '../store/appointmentStore';

export default function AppointmentForm() {
  const [formData, setFormData] = useState({
    doctor: '',
    date: '',
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [success, setSuccess] = useState(false);

  const addAppointment = useAppointmentStore((state) => state.addAppointment);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError(null);
    setSuccess(false);

    try {
      const response = await appointmentsAPI.createAppointment(formData);
      
      if (response.data?.appointment) {
        addAppointment(response.data.appointment);
        setFormData({ doctor: '', date: '' });
        setSuccess(true);
        setTimeout(() => setSuccess(false), 3000);
      }
    } catch (err) {
      setError(err.response?.data?.error || 'Ошибка при создании записи');
    } finally {
      setLoading(false);
    }
  };

  const handleChange = (e) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: value,
    }));
  };

  return (
    <div className="card">
      <h3 className="text-xl font-bold text-slate-900 dark:text-white mb-4">
        Новая запись
      </h3>

      <form onSubmit={handleSubmit} className="space-y-4">
        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
            Врач
          </label>
          <input
            type="text"
            name="doctor"
            placeholder="Введите имя врача"
            className="form-input"
            value={formData.doctor}
            onChange={handleChange}
            required
            disabled={loading}
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-2">
            Дата и время
          </label>
          <input
            type="datetime-local"
            name="date"
            className="form-input"
            value={formData.date}
            onChange={handleChange}
            required
            disabled={loading}
          />
        </div>

        {error && (
          <div className="bg-red-100 dark:bg-red-900 border border-red-400 dark:border-red-700 text-red-700 dark:text-red-200 px-3 py-2 rounded-lg text-sm">
            {error}
          </div>
        )}

        {success && (
          <div className="bg-green-100 dark:bg-green-900 border border-green-400 dark:border-green-700 text-green-700 dark:text-green-200 px-3 py-2 rounded-lg text-sm">
            Запись создана ✓
          </div>
        )}

        <button
          type="submit"
          className="btn-primary w-full"
          disabled={loading}
        >
          {loading ? 'Создание...' : 'Записаться'}
        </button>
      </form>
    </div>
  );
}
