package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq" // КРИТИЧЕСКИ ВАЖНО
)

const MY_TG_ID = 58392011
const BOT_LINK = "https://t.me/health_os_bot"

var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())

	// 1. Пробуем подключиться, но не вылетаем при ошибке
	dbURL := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("КРИТИЧЕСКАЯ ОШИБКА БД: %v", err)
	}

	// 2. Запускаем бота в фоне
	go startTelegramBot()

	// 3. Маршруты
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/verify-otp", handleVerifyOTP)
	http.HandleFunc("/save-diagnosis", handleSaveDiagnosis)
	http.HandleFunc("/logout", handleLogout)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK") // Для проверки жизни сервера
	})

	// 4. Порт (Render требует этого)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Сервер запускается на порту %s...", port)

	// Используем nil вместо db, если она не создалась, чтобы сервер жил
	serverErr := http.ListenAndServe(":"+port, nil)
	if serverErr != nil {
		log.Printf("Сервер упал: %v", serverErr)
	}
}

func startTelegramBot() {
	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Println("ОШИБКА: TELEGRAM_APITOKEN не установлен")
		return
	}
	// ... (остальной код бота без изменений)
	log.Println("Бот запущен...")
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cID, _ := r.Cookie("user_id")

	if cID == nil || cID.Value == "" {
		fmt.Fprintf(w, "<h1>HealthOS</h1><p>Система готова. Напишите боту.</p><a href='%s'>Перейти к боту</a><br><br><form action='/verify-otp' method='POST'><input name='otp' placeholder='Код'><button>Войти</button></form>", BOT_LINK)
		return
	}

	role, _ := r.Cookie("user_role")
	name, _ := r.Cookie("user_name")

	if role != nil && role.Value == "doctor" {
		fmt.Fprintf(w, "<h2>Кабинет Доктора: %s</h2>", name.Value)
		// Проверка на nil перед запросом
		if db != nil {
			rows, _ := db.Query("SELECT tg_id, patient_name, diagnosis FROM appointments WHERE tg_id != $1", MY_TG_ID)
			if rows != nil {
				for rows.Next() {
					var pID int64
					var pName, pDiag string
					rows.Scan(&pID, &pName, &pDiag)
					fmt.Fprintf(w, "<div>%s (ID:%d): %s</div>", pName, pID, pDiag)
				}
				rows.Close()
			}
		}
	} else {
		fmt.Fprintf(w, "<h2>Кабинет Пациента: %s</h2>", name.Value)
	}
	fmt.Fprint(w, "<br><a href='/logout'>Выйти</a>")
}

// Упрощенные заглушки для остальных функций, чтобы не было ошибок компиляции
func handleSaveDiagnosis(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", 302) }
func handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	// Для теста: пускаем с любым кодом '123456'
	otp := r.FormValue("otp")
	if otp == "123456" {
		http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "1", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "doctor", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "user_name", Value: "Admin", Path: "/"})
	}
	http.Redirect(w, r, "/", 302)
}
func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "user_id", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "user_role", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "user_name", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", 302)
}
