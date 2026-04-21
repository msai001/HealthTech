#!/bin/bash

# HealthTech Development Setup
# Скрипт для быстрого запуска проекта в режиме разработки

echo "🚀 Starting HealthTech Development Environment"
echo ""

# Проверяем переменные окружения
if [ -z "$DATABASE_URL" ]; then
    echo "⚠️  WARNING: DATABASE_URL не установлена"
    echo "   Установите переменные окружения перед запуском"
fi

if [ -z "$GOOGLE_CLIENT_ID" ] || [ -z "$GOOGLE_CLIENT_SECRET" ]; then
    echo "⚠️  WARNING: GOOGLE_CLIENT_ID или GOOGLE_CLIENT_SECRET не установлены"
fi

# Запускаем backend в фоне
echo "📦 Starting backend on port 8080..."
go run ./cmd/main.go &
BACKEND_PID=$!

# Даём время на запуск backend
sleep 2

# Запускаем frontend в фоне
echo "⚛️  Starting frontend on port 5173..."
cd frontend
npm run dev &
FRONTEND_PID=$!

echo ""
echo "✅ Development servers started!"
echo "   Backend:  http://localhost:8080"
echo "   Frontend: http://localhost:5173"
echo ""
echo "   Press Ctrl+C to stop both servers"
echo ""

# Перехватываем Ctrl+C и закрываем оба сервера
trap "kill $BACKEND_PID $FRONTEND_PID 2>/dev/null; exit" INT

# Ждём завершения
wait
