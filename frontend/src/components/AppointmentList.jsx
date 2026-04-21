import { useState } from 'react';
import { appointmentsAPI } from '../services/api';
import useAppointmentStore from '../store/appointmentStore';

export default function AppointmentList({ appointments }) {
  const [deleting, setDeleting] = useState(null);
  const removeAppointment = useAppointmentStore(
    (state) => state.removeAppointment
  );

  const handleDelete = async (id) => {
    if (!confirm('Вы уверены?')) return;

    setDeleting(id);
    try {
      await appointmentsAPI.deleteAppointment(id);
      removeAppointment(id);
    } catch (err) {
      console.error('Error deleting appointment:', err);
      alert('Ошибка при удалении записи');
    } finally {
      setDeleting(null);
    }
  };

  const formatDate = (dateString) => {
    const date = new Date(dateString);
    return new Intl.DateTimeFormat('ru-RU', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }).format(date);
  };

  return (
    <div className="space-y-3">
      {appointments.map((appointment) => (
        <div
          key={appointment.id}
          className="item"
        >
          <button
            onClick={() => handleDelete(appointment.id)}
            className="del"
            disabled={deleting === appointment.id}
          >
            {deleting === appointment.id ? '⏳' : '✕'}
          </button>
          <div>
            <h4 className="font-semibold text-slate-900 dark:text-white">
              {appointment.doctor_name}
            </h4>
            <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
              {formatDate(appointment.appointment_date)}
            </p>
          </div>
        </div>
      ))}
    </div>
  );
}
