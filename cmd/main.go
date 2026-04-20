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
		:root { --primary: #10b981; --bg: #f8fafc; }
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: var(--bg); display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
		.card { background: white; padding: 40px; border-radius: 32px; box-shadow: 0 20px 40px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 450px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 14px; background: var(--primary); color: white; width: 100%; font-size: 16px; margin-top: 15px; text-decoration: none; display: inline-block; }
		.otp-input { font-size: 28px; letter-spacing: 8px; text-align: center; width: 80%; margin-top: 20px; border: 2px solid #e2e8f0; border-radius: 12px; padding: 10px; }
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

func sendEmailOTP(toEmail, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")

	if from == "" || pass == "" {
		log.Println("ОШИБКА: Переменные EMAIL_USER или EMAIL_PASS все еще не видны коду!")
		return
	}

	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := fmt.Sprintf("From: %s\nTo: %s\nSubject: HealthTech Security Code\n\nYour code is: %s", from, toEmail, code)

	err := smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(msg))
	if err != nil {
		log.Printf("Ошибка SMTP: %v", err)
	} else {
		log.Printf("УСПЕХ: Код отправлен на %s", toEmail)
	}
}

func main() {
	initDB()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
		fmt.Fprintf(w, "<html><head><meta charset=\"UTF-8\">%s</head><body><div class=\"card\"><h1>🌿 HealthTech</h1><a href=\"%s\" class=\"btn\">Войти через Google</a></div></body></html>", sharedStyles, url)
	})

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, _ := googleOAuthConfig.Exchange(context.Background(), code)
		client := googleOAuthConfig.Client(context.Background(), token)
		resp, _ := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		var userInfo struct{ Email string }
		json.NewDecoder(resp.Body).Decode(&userInfo)

		cookie := &http.Cookie{Name: "otp_pending", Value: userInfo.Email, Path: "/", Expires: time.Now().Add(15 * time.Minute)}
		http.SetCookie(w, cookie)
		http.Redirect(w, r, "/otp-verify", 302)
	})

	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("otp_pending")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}
		email := cookie.Value

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND totp_secret != '' LIMIT 1", email).Scan(&secret)
		if secret == "" {
			key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
			secret = key.Secret()
			db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'System', '2026-01-01', 'Admin')", email, secret)
		}

		currentCode, _ := totp.GenerateCode(secret, time.Now())
		go sendEmailOTP(email, currentCode)

		qrUrl := fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=120x120&data=otpauth://totp/HealthTech:%s?secret=%s&issuer=HealthTech", email, secret)

		fmt.Fprintf(w, "<html><head><meta charset=\"UTF-8\">%s</head><body><div class=\"card\"><h2>Введите код</h2><img src=\"%s\" style=\"margin:20px 0;\"><p>Код отправлен на почту</p><form action=\"/otp-check\" method=\"POST\"><input type=\"text\" name=\"code\" class=\"otp-input\" maxlength=\"6\" required autofocus><button type=\"submit\" class=\"btn\">Подтвердить</button></form></div></body></html>", sharedStyles, qrUrl)
	})

	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("otp_pending")
		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND totp_secret != '' LIMIT 1", cookie.Value).Scan(&secret)

		// Исправлено: принимаем два значения
		valid, _ := totp.ValidateCustom(r.FormValue("code"), secret, time.Now(), totp.ValidateOpts{Skew: 1})

		if valid {
			fmt.Fprintf(w, "<html><body><h1>Успешный вход!</h1></body></html>")
		} else {
			fmt.Fprintf(w, "<script>alert('Неверный код'); window.history.back();</script>")
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
