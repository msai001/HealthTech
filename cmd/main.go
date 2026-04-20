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

var googleOAuthConfig = &oauth2.Config{
	ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
	ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	RedirectURL:  "https://healthtech-1.onrender.com/callback",
	Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
	Endpoint:     google.Endpoint,
}

var db *sql.DB

const sharedStyles = `
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;600;700&display=swap');
		:root { --primary: #10b981; --bg: #f8fafc; --text: #0f172a; }
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: var(--bg); color: var(--text); display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
		.card { background: white; padding: 40px; border-radius: 24px; box-shadow: 0 10px 30px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 400px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 14px; background: var(--primary); color: white; width: 100%; font-size: 16px; margin-top: 15px; text-decoration: none; display: block; }
		input { width: 100%; padding: 14px; border: 2px solid #f1f5f9; border-radius: 12px; font-size: 24px; text-align: center; outline: none; margin-top: 15px; letter-spacing: 5px; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	if err = db.Ping(); err != nil {
		db, _ = sql.Open("postgres", connStr+"?sslmode=require")
	}
}

// Функция отправки письма
func sendEmailOTP(toEmail, code string) error {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")

	msg := "From: " + from + "\n" +
		"To: " + toEmail + "\n" +
		"Subject: HealthTech Verification Code\n\n" +
		"Your security code is: " + code + "\n" +
		"It will expire in 5 minutes."

	err := smtp.SendMail("smtp.gmail.com:587",
		smtp.PlainAuth("", from, pass, "smtp.gmail.com"),
		from, []string{toEmail}, []byte(msg))
	return err
}

func getCookie(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func main() {
	initDB()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="card"><h1>🌿 HealthTech</h1><a href="%s" class="btn">Войти через Google</a></div>
		</body></html>`, sharedStyles, url)
	})

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, err := googleOAuthConfig.Exchange(context.Background(), code)
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)

		// Генерируем код (6 цифр)
		otpCode := fmt.Sprintf("%06d", rand.Intn(1000000))

		// Сохраняем код в базу (используем totp_secret как временное хранилище кода)
		db.Exec("UPDATE appointments SET totp_secret = $1 WHERE user_email = $2", otpCode, userInfo.Email)
		// Если записи нет - создаем
		db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) SELECT $1, $2, 'System', '2026-01-01', 'Admin' WHERE NOT EXISTS (SELECT 1 FROM appointments WHERE user_email = $3)", userInfo.Email, otpCode, userInfo.Email)

		// Отправляем на почту
		sendEmailOTP(userInfo.Email, otpCode)

		http.SetCookie(w, &http.Cookie{Name: "otp_pending", Value: userInfo.Email, Path: "/", Expires: time.Now().Add(10 * time.Minute)})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		email := getCookie(r, "otp_pending")
		fmt.Fprintf(w, `<html><head><meta charset="UTF-8">%s</head><body>
			<div class="card">
				<h1>📧 Код на почте</h1>
				<p>Мы отправили код на %s</p>
				<form action="/otp-check" method="POST">
					<input type="text" name="code" placeholder="000000" maxlength="6" required>
					<button type="submit" class="btn">Проверить код</button>
				</form>
			</div>
		</body></html>`, sharedStyles, email)
	})

	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		email := getCookie(r, "otp_pending")
		userCode := r.FormValue("code")

		var dbCode string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 ORDER BY id DESC LIMIT 1", email).Scan(&dbCode)

		if userCode == dbCode && userCode != "" {
			http.SetCookie(w, &http.Cookie{Name: "user_session", Value: email, Path: "/", Expires: time.Now().Add(24 * time.Hour)})
			http.Redirect(w, r, "/dashboard", 302)
		} else {
			fmt.Fprintf(w, `<script>alert("Неверный код!"); window.history.back();</script>`)
		}
	})

	// ... Тут функции /dashboard, /save, /delete из прошлого кода ...
	// (Оставь их без изменений из предыдущего ответа)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
