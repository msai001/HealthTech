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
	Scopes: []string{
		"openid",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	},
	Endpoint: google.Endpoint,
}

var db *sql.DB

const sharedStyles = `
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;600;700&display=swap');
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: #f1f5f9; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
		.card { background: white; padding: 40px; border-radius: 24px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); text-align: center; width: 350px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 14px; background: #10b981; color: white; width: 100%; display: block; text-decoration: none; margin-top: 15px; }
		.otp-input { font-size: 32px; text-align: center; width: 100%; padding: 10px; border: 2px solid #e2e8f0; border-radius: 12px; margin: 20px 0; outline: none; letter-spacing: 5px; }
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
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := fmt.Sprintf("Subject: HealthTech Code: %s\r\n\r\nYour verification code is: %s", code, code)
	smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(msg))
}

func main() {
	initDB()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>🌿 HealthTech</h1><a href='%s' class='btn'>Войти через Google</a></div></body></html>", sharedStyles, url)
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

		http.SetCookie(w, &http.Cookie{Name: "user_email", Value: userInfo.Email, Path: "/", Expires: time.Now().Add(15 * time.Minute)})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		email := cookie.Value
		if email == "" {
			http.Redirect(w, r, "/", 302)
			return
		}

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 ORDER BY id DESC LIMIT 1", email).Scan(&secret)

		if secret == "" {
			key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
			secret = key.Secret()
			db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'User', '2026-01-01', 'System')", email, secret)
		}

		// Принудительно чистим секрет
		secret = strings.TrimSpace(secret)

		code, _ := totp.GenerateCode(secret, time.Now())
		go sendEmailOTP(email, code)

		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Введите код</h2><p>Проверьте почту %s</p><form action='/otp-check' method='POST'><input type='text' name='code' class='otp-input' required autofocus><button type='submit' class='btn'>Войти</button></form></div></body></html>", sharedStyles, email)
	})

	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		userCode := strings.TrimSpace(r.FormValue("code"))

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 ORDER BY id DESC LIMIT 1", cookie.Value).Scan(&secret)

		secret = strings.TrimSpace(secret)

		// Skew 3 (запас 1.5 минуты). Проверка на SHA1 (по умолчанию в этой библиотеке)
		valid, _ := totp.ValidateCustom(userCode, secret, time.Now(), totp.ValidateOpts{
			Skew: 3, Digits: 6, Period: 30, Algorithm: 0,
		})

		if valid {
			fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>✅ Успех</h1><p>Вы авторизованы!</p></div></body></html>", sharedStyles)
		} else {
			fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>❌ Ошибка</h1><p>Код не подошел.</p><a href='/otp-verify' class='btn'>Еще раз</a></div></body></html>", sharedStyles)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
