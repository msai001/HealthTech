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
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
		.card { background: white; padding: 40px; border-radius: 24px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 400px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 14px; background: #10b981; color: white; width: 100%; display: inline-block; text-decoration: none; margin-top: 10px; }
		.otp-input { font-size: 24px; text-align: center; width: 100%; padding: 12px; border: 2px solid #e2e8f0; border-radius: 12px; margin: 20px 0; outline: none; }
	</style>
`

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(5)
}

func sendEmailOTP(toEmail, code string) {
	from := os.Getenv("EMAIL_USER")
	pass := os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := fmt.Sprintf("Subject: HealthTech Code: %s\n\nYour code: %s", code, code)
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
		cookie, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}
		email := cookie.Value

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 ORDER BY id DESC LIMIT 1", email).Scan(&secret)

		if secret == "" {
			key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
			secret = key.Secret()
			db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'User', '2026-01-01', 'System')", email, secret)
		}

		code, _ := totp.GenerateCode(secret, time.Now())
		go sendEmailOTP(email, code)

		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Введите код</h2><p>Мы отправили его на почту</p><form action='/otp-check' method='POST'><input type='text' name='code' class='otp-input' required><button type='submit' class='btn'>Войти</button></form></div></body></html>", sharedStyles)
	})

	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		var secret string
		err = db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 ORDER BY id DESC LIMIT 1", cookie.Value).Scan(&secret)

		if err != nil || secret == "" {
			fmt.Fprintf(w, "Ошибка: секрет не найден. <a href='/'>На главную</a>")
			return
		}

		// Skew 2 позволяет вводить код, даже если время на сервере и телефоне чуть-чуть разное
		valid, _ := totp.ValidateCustom(r.FormValue("code"), secret, time.Now(), totp.ValidateOpts{
			Skew: 2, Digits: 6, Period: 30, Algorithm: 0,
		})

		if valid {
			fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>✅ Успех!</h1><p>Вы зашли в систему.</p></div></body></html>", sharedStyles)
		} else {
			fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>❌ Ошибка</h1><p>Неверный код.</p><a href='/otp-verify' class='btn'>Попробовать еще раз</a></div></body></html>", sharedStyles)
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
