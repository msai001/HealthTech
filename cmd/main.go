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

func getOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  "https://healthtech-1.onrender.com/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	rand.Seed(time.Now().UnixNano())

	// Хендлер входа
	http.HandleFunc("/api/auth/google", func(w http.ResponseWriter, r *http.Request) {
		config := getOAuthConfig()
		if config.ClientID == "" {
			http.Error(w, "Критическая ошибка: GOOGLE_CLIENT_ID не найден в настройках Render!", 500)
			return
		}
		url := config.AuthCodeURL("state")
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})

	http.HandleFunc("/callback", handleCallback)
	http.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting v1.6.1")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	config := getOAuthConfig()
	token, err := config.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Ошибка обмена токена: "+err.Error(), 500)
		return
	}

	client := config.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "Ошибка данных пользователя", 500)
		return
	}
	defer resp.Body.Close()

	var user struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&user)

	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	db.Exec("INSERT INTO appointments (user_email, totp_secret, doctor_name, appointment_date, patient_name) VALUES ($1, $2, 'System', '2026-04-24', 'User')", user.Email, otp)

	http.SetCookie(w, &http.Cookie{Name: "user_email", Value: user.Email, Path: "/"})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<div style='text-align:center; padding:50px;'><h1>Успех!</h1><p>Email: %s</p><h2>Твой код для pgAdmin: %s</h2><a href='/'>Вернуться на главную</a></div>", user.Email, otp)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
		<div style="text-align:center; padding: 50px; font-family: sans-serif;">
			<h1>Health Monitoring System</h1>
			<p>Статус: Работает</p>
			<a href="/api/auth/google" style="padding: 15px 25px; background: #4285F4; color: white; text-decoration: none; border-radius: 5px; font-weight: bold; display: inline-block;">ВОЙТИ ЧЕРЕЗ GOOGLE</a>
			<p style="margin-top: 20px; color: #bdc3c7;">v1.6.1</p>
		</div>
	`)
}
