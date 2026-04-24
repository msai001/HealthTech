package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/smtp"
	"os"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// --- CONFIGURATION ---
var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	// ИСПРАВЛЕНО: Теперь совпадает с твоим скриншотом Google Console (image_a47b3b.png)
	RedirectURL: "https://healthtech-1.onrender.com/callback",
	Scopes:      []string{"openid", "email", "profile"},
	Endpoint:    google.Endpoint,
}

var db *sql.DB

// --- MODELS ---
type AppointmentResponse struct {
	ID              int    `json:"id"`
	DoctorName      string `json:"doctor_name"`
	AppointmentDate string `json:"appointment_date"`
}

type UserResponse struct {
	Email string `json:"email"`
}

// --- HELPERS ---
func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("DB connection failed: ", err)
	}
}

func sendMail(to, subject, body string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := []byte("Subject: " + subject + "\r\n\r\n" + body)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{to}, msg)
}

// --- MAIN ---
func main() {
	initDB()
	rand.Seed(time.Now().UnixNano())

	// Роут для начала авторизации
	http.HandleFunc("/api/auth/google", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})

	// Роут колбэка (должен совпадать с RedirectURL)
	http.HandleFunc("/callback", handleOAuthCallback)

	// Остальные роуты
	http.HandleFunc("/api/auth/verify-otp", handleOTPVerify)
	http.HandleFunc("/", handleLegacyRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server v1.5.6 starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- HANDLERS ---

func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Code not found", http.StatusBadRequest)
		return
	}

	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Exchange error: %v", err)
		http.Error(w, "Token exchange failed", http.StatusUnauthorized)
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&user)

	// Сохраняем OTP в базу (как в твоем pgAdmin)
	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", user.Email)
	db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, appointment_date, patient_name) VALUES ($1, $2, 'System', '2026-01-01', 'User')", user.Email, otp)

	// Отправка почты (асинхронно)
	go sendMail(user.Email, "HealthTech Login Code", "Your code: "+otp)

	// Ставим куку
	http.SetCookie(w, &http.Cookie{
		Name: "user_email", Value: user.Email, Path: "/", MaxAge: 86400,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})

	// Возвращаем на главную
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// ... (логика проверки OTP)
}

func handleLegacyRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<!DOCTYPE html>
		<html lang="ru">
		<head>
			<meta charset="UTF-8">
			<title>Health Monitoring</title>
			<style>
				body { font-family: sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f4f7f6; }
				.card { background: white; padding: 40px; border-radius: 12px; box-shadow: 0 4px 20px rgba(0,0,0,0.1); text-align: center; }
				.btn { background: #4285F4; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; display: inline-block; transition: background 0.2s; }
				.btn:hover { background: #357ae8; }
			</style>
		</head>
		<body>
			<div class="card">
				<h1>Система HealthTech</h1>
				<p>Вы успешно подключены к серверу.</p>
				<a href="/api/auth/google" class="btn">Войти через Google</a>
				<p style="color: #bdc3c7; font-size: 0.8em; margin-top: 20px;">v1.5.6 | Database Connected</p>
			</div>
		</body>
		</html>
	`)
}
