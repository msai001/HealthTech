import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { authAPI } from '../services/api';
import useAuthStore from '../store/authStore';

export default function OTPVerify() {
  const [code, setCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const navigate = useNavigate();
  const setUser = useAuthStore((state) => state.setUser);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setLoading(true);
    setError(null);

    try {
      const response = await authAPI.verifyOTP(code);
      
      if (response.data?.email) {
        setUser(response.data.email);
        navigate('/dashboard');
      } else {
        setError('Ошибка при проверке кода');
      }
    } catch (err) {
      setError(err.response?.data?.error || 'Неправильный код');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-emerald-50 to-blue-50 dark:from-slate-900 dark:to-slate-800 flex items-center justify-center p-4">
      <div className="card w-full max-w-md">
        <h2 className="text-2xl font-bold text-slate-900 dark:text-white mb-2">
          Проверка кода
        </h2>
        <p className="text-slate-500 dark:text-slate-400 mb-6">
          Введите 6-значный код из письма
        </p>

        <form onSubmit={handleSubmit} className="space-y-4">
          <input
            type="text"
            placeholder="000000"
            className="form-input text-center text-2xl tracking-widest"
            value={code}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
            maxLength="6"
            required
            disabled={loading}
          />

          {error && (
            <div className="bg-red-100 dark:bg-red-900 border border-red-400 dark:border-red-700 text-red-700 dark:text-red-200 px-4 py-3 rounded-lg text-sm">
              {error}
            </div>
          )}

          <button
            type="submit"
            className="btn-primary w-full"
            disabled={loading || code.length !== 6}
          >
            {loading ? 'Проверка...' : 'Подтвердить'}
          </button>
        </form>

        <p className="text-center text-sm text-slate-500 dark:text-slate-400 mt-4">
          Код был отправлен на вашу почту
        </p>
      </div>
    </div>
  );
}
