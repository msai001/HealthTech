package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
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
	// СТРОГО КАК НА СКРИНШОТЕ image_a47b3b.png
	RedirectURL: "https://healthtech-1.onrender.com/callback",
	Scopes:      []string{"openid", "email", "profile"},
	Endpoint:    google.Endpoint,
}

var db *sql.DB

// --- INITIALIZATION ---
func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("DB Error: ", err)
	}
}

// --- MAIN ---
func main() {
	initDB()
	rand.Seed(time.Now().UnixNano())

	// Хендлер для кнопки "Войти"
	http.HandleFunc("/api/auth/google", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})

	// Хендлер для возврата из Google (маршрут /callback)
	http.HandleFunc("/callback", handleOAuthCallback)

	// Главная страница и проверка OTP
	http.HandleFunc("/api/auth/verify-otp", handleOTPVerify)
	http.HandleFunc("/", handleLegacyRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server Version 1.5.7 is live on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// --- HANDLERS ---
func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "No code in request", http.StatusBadRequest)
		return
	}

	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("OAuth Exchange Error: %v", err)
		http.Error(w, "Auth failed", http.StatusUnauthorized)
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "User info error", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&user)

	// Генерируем OTP и пишем в БД (как в твоем pgAdmin)
	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", user.Email)
	db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, appointment_date, patient_name) VALUES ($1, $2, 'System', '2026-01-01', 'User')", user.Email, otp)

	// Ставим куку и редиректим на главную
	http.SetCookie(w, &http.Cookie{
		Name: "user_email", Value: user.Email, Path: "/", MaxAge: 86400,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	// Твоя логика проверки кода
}

func handleLegacyRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<!DOCTYPE html>
		<html>
		<head>
			<meta charset="UTF-8">
			<title>HealthTech Auth</title>
			<style>
				body { font-family: sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #eceff1; }
				.box { background: white; padding: 40px; border-radius: 12px; box-shadow: 0 4px 10px rgba(0,0,0,0.1); text-align: center; }
				.btn { background: #4285F4; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; display: inline-block; }
			</style>
		</head>
		<body>
			<div class="box">
				<h1>Health Monitoring</h1>
				<p>Авторизация в системе</p>
				<a href="/api/auth/google" class="btn">Войти через Google</a>
				<p style="color: #90a4ae; font-size: 0.8em; margin-top: 20px;">Backend v1.5.7</p>
			</div>
		</body>
		</html>
	`)
}
