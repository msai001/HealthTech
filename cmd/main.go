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
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: #f8fafc; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 20px; }
		.card { background: white; padding: 30px; border-radius: 24px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 450px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 14px; background: #10b981; color: white; width: 100%; display: block; text-decoration: none; margin-top: 10px; transition: 0.2s; }
		.btn:hover { background: #059669; }
		.otp-input, .form-input { font-size: 18px; width: 100%; padding: 12px; border: 2px solid #e2e8f0; border-radius: 12px; margin: 10px 0; outline: none; box-sizing: border-box; }
		.appointment-card { border-left: 4px solid #10b981; background: #f9fafb; padding: 15px; margin-bottom: 15px; border-radius: 8px; text-align: left; }
		label { display: block; text-align: left; font-weight: 600; color: #475569; margin-top: 10px; }
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
	msg := fmt.Sprintf("Subject: HealthTech Code: %s\r\n\r\nVerification code: %s", code, code)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(msg))
}

func main() {
	initDB()

	// 1. ГЛАВНАЯ СТРАНИЦА
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := googleOAuthConfig.AuthCodeURL("state", oauth2.SetAuthURLParam("prompt", "select_account"))
		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h1>🌿 HealthTech</h1><p>Система управления записями</p><a href='%s' class='btn'>Войти через Google</a></div></body></html>", sharedStyles, url)
	})

	// 2. CALLBACK
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
		http.SetCookie(w, &http.Cookie{Name: "user_email", Value: userInfo.Email, Path: "/", Expires: time.Now().Add(30 * time.Minute)})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	// 3. OTP VERIFY (Генерация кода)
	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		email := cookie.Value
		if email == "" {
			http.Redirect(w, r, "/", 302)
			return
		}

		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND totp_secret != '' ORDER BY id DESC LIMIT 1", email).Scan(&secret)
		if secret == "" {
			key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
			secret = key.Secret()
			db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'Новый пользователь', '2026-01-01', 'Система')", email, secret)
		}
		otp, _ := totp.GenerateCode(strings.TrimSpace(secret), time.Now())
		go sendEmailOTP(email, otp)
		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Введите код</h2><p>Отправлено на %s</p><form action='/otp-check' method='POST'><input type='text' name='code' class='otp-input' required autofocus><button type='submit' class='btn'>Подтвердить</button></form></div></body></html>", sharedStyles, email)
	})

	// 4. OTP CHECK + ЛИЧНЫЙ КАБИНЕТ
	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND totp_secret != '' ORDER BY id DESC LIMIT 1", cookie.Value).Scan(&secret)
		valid, _ := totp.ValidateCustom(strings.TrimSpace(r.FormValue("code")), strings.TrimSpace(secret), time.Now(), totp.ValidateOpts{Skew: 3, Digits: 6, Period: 30})

		if valid {
			rows, _ := db.Query("SELECT doctor_name, appointment_date, patient_name FROM appointments WHERE user_email = $1", cookie.Value)
			var listHTML string
			for rows.Next() {
				var d, dt, p string
				rows.Scan(&d, &dt, &p)
				if d != "Система" { // Скрываем системную запись с секретом
					listHTML += fmt.Sprintf("<div class='appointment-card'><strong>👨‍⚕️ %s</strong><br><small>📅 %s</small><br>👤 %s</div>", d, dt, p)
				}
			}
			if listHTML == "" {
				listHTML = "<p>У вас нет записей</p>"
			}

			fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Личный кабинет</h2><p>%s</p><h3>Мои записи:</h3>%s<hr><form action='/add-appointment' method='POST'><h3>Записаться к врачу</h3><label>Имя врача</label><input name='doc' class='form-input' required><label>Дата и время</label><input name='date' class='form-input' placeholder='20.05.2026 10:00' required><button class='btn'>Записаться</button></form><a href='/' style='display:block; margin-top:20px; color:#64748b;'>Выйти</a></div></body></html>", sharedStyles, cookie.Value, listHTML)
		} else {
			fmt.Fprintf(w, "<script>alert('Неверно'); window.history.back();</script>")
		}
	})

	// 5. ДОБАВЛЕНИЕ ЗАПИСИ
	http.HandleFunc("/add-appointment", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		if r.Method == "POST" && cookie.Value != "" {
			db.Exec("INSERT INTO appointments (user_email, doctor_name, appointment_date, patient_name, totp_secret) VALUES ($1, $2, $3, $4, '')", cookie.Value, r.FormValue("doc"), r.FormValue("date"), "Пациент")
		}
		fmt.Fprintf(w, "<html><body><script>alert('Запись создана!'); window.location.href='/otp-check?code=INTERNAL';</script></body></html>")
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
