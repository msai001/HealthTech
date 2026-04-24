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

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	// ТОЧНО как в твоей консоли Google (скрин image_a47b3b.png)
	RedirectURL: "https://healthtech-1.onrender.com/callback",
	Scopes:      []string{"openid", "email", "profile"},
	Endpoint:    google.Endpoint,
}

var db *sql.DB

func main() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Ошибка БД: ", err)
	}

	rand.Seed(time.Now().UnixNano())

	// Роуты
	http.HandleFunc("/api/auth/google", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state")
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})

	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Запуск сервера v1.5.9 на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	token, err := googleOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Ошибка обмена токена: %v", err)
		http.Error(w, "Auth failed", http.StatusUnauthorized)
		return
	}

	client := googleOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "Get user info failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user struct{ Email string }
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		http.Error(w, "Decode failed", http.StatusInternalServerError)
		return
	}

	// Запись в базу для pgAdmin
	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, appointment_date, patient_name) VALUES ($1, $2, 'System', '2026-04-24', 'User')", user.Email, otp)

	http.SetCookie(w, &http.Cookie{Name: "user_email", Value: user.Email, Path: "/"})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<div style="text-align:center; padding: 50px; font-family: sans-serif; background: #f0f2f5; height: 100vh;">
			<div style="background: white; display: inline-block; padding: 40px; border-radius: 10px; box-shadow: 0 2px 10px rgba(0,0,0,0.1);">
				<h1>Health Monitoring System</h1>
				<p>Нажми кнопку ниже для входа</p>
				<a href="/api/auth/google" style="padding: 15px 25px; background: #4285F4; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; display: inline-block; margin-top: 20px;">ВОЙТИ ЧЕРЕЗ GOOGLE</a>
				<p style="margin-top: 30px; color: #bdc3c7; font-size: 0.8em;">Версия системы: 1.5.9</p>
			</div>
		</div>
	`)
}
