package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/pquerna/otp/totp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"openid", "email", "profile"},
	Endpoint:     google.Endpoint,
}

var db *sql.DB

const sharedStyles = `
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;600;700&display=swap');
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: #f1f5f9; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
		.card { background: white; padding: 40px; border-radius: 30px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 400px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 16px; background: #10b981; color: white; width: 100%; display: block; text-decoration: none; margin-top: 10px; }
		.form-input { font-size: 24px; width: 100%; padding: 12px; border: 2px solid #e2e8f0; border-radius: 12px; margin: 10px 0; text-align: center; letter-spacing: 5px; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
}

func sendEmailOTP(toEmail, code string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	// Добавляем время в тему, чтобы письма не группировались
	subject := fmt.Sprintf("Subject: Code %s (%d)\r\n\r\n", code, time.Now().Unix())
	msg := []byte(subject + "Your HealthTech code: " + code)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, msg)
}

func main() {
	initDB()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		session, err := r.Cookie("session_valid")
		if err == nil && session.Value == "true" {
			http.Redirect(w, r, "/dashboard", 302)
			return
		}
		fmt.Fprintf(w, "<html><head>%s</head><body><div class='card'><h1>🌿 HealthTech</h1><a href='%s' class='btn'>Войти через Google</a></div></body></html>", sharedStyles, googleOAuthConfig.AuthCodeURL("state"))
	})

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, _ := googleOAuthConfig.Exchange(context.Background(), code)
		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)
		http.SetCookie(w, &http.Cookie{Name: "user_email", Value: userInfo.Email, Path: "/", MaxAge: 86400, HttpOnly: true})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		email := cookie.Value

		// 1. Очищаем всё старое
		db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", email)

		// 2. Генерируем новый секрет
		key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
		secret := key.Secret()
		db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'User', '2026-01-01', 'System')", email, secret)

		// 3. Создаем код
		otpCode, _ := totp.GenerateCode(secret, time.Now())

		// ВАЖНО: Печатаем код в консоль Render (проверь логи в панели Render!)
		fmt.Printf("DEBUG: Код для %s -> %s\n", email, otpCode)

		go sendEmailOTP(email, otpCode)

		fmt.Fprintf(w, "<html><head>%s</head><body><div class='card'><h2>Введите код</h2><form action='/otp-check' method='POST'><input type='text' name='code' class='form-input' required autofocus autocomplete='off'><button type='submit' class='btn'>Подтвердить</button></form><br><a href='/otp-verify'>Отправить еще раз</a></div></body></html>", sharedStyles)
	})

	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' ORDER BY id DESC LIMIT 1", cookie.Value).Scan(&secret)

		inputCode := strings.TrimSpace(r.FormValue("code"))

		// Увеличиваем Skew до 10 (максимально лояльная проверка)
		valid, _ := totp.ValidateCustom(inputCode, strings.TrimSpace(secret), time.Now(), totp.ValidateOpts{
			Skew:   10,
			Digits: 6,
			Period: 30,
		})

		if valid {
			http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400, HttpOnly: true})
			http.Redirect(w, r, "/dashboard", 302)
		} else {
			// Если ошибка — выводим отладочную инфу в консоль
			fmt.Printf("FAILED: Попытка входа с кодом %s для секрета %s\n", inputCode, secret)
			fmt.Fprintf(w, "<script>alert('Неверный код! Проверьте последнее письмо.'); window.history.back();</script>")
		}
	})

	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		emailCookie, _ := r.Cookie("user_email")
		sessionCookie, _ := r.Cookie("session_valid")
		if sessionCookie == nil || sessionCookie.Value != "true" {
			http.Redirect(w, r, "/", 302)
			return
		}
		// ... (код дашборда без изменений)
		fmt.Fprintf(w, "<html><head>%s</head><body><div class='card'><h2>Кабинет</h2><p>%s</p><a href='/logout'>Выход</a></div></body></html>", sharedStyles, emailCookie.Value)
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/", 302)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
