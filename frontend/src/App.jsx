import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { useEffect } from 'react';
import Login from './components/Login';
import OTPVerify from './components/OTPVerify';
import Dashboard from './components/Dashboard';
import useAuthStore from './store/authStore';
import { authAPI } from './services/api';

function App() {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const setUser = useAuthStore((state) => state.setUser);
  const user = useAuthStore((state) => state.user);

  // Проверяем аутентификацию при загрузке
  useEffect(() => {
    const checkAuth = async () => {
      try {
        const response = await authAPI.getCurrentUser();
        if (response.data?.email) {
          setUser(response.data.email);
        }
      } catch (err) {
        console.log('Not authenticated');
      }
    };

    checkAuth();
  }, [setUser]);

  return (
    <Router>
      <Routes>
        <Route path="/" element={!user ? <Login /> : <Navigate to="/dashboard" />} />
        <Route path="/otp-verify" element={!user ? <OTPVerify /> : <Navigate to="/dashboard" />} />
        <Route path="/dashboard" element={user ? <Dashboard /> : <Navigate to="/" />} />
        <Route path="/callback" element={<OTPVerify />} />
        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </Router>
  );
}

export default App;
