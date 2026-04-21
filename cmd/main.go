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
		.card { background: white; padding: 30px; border-radius: 24px; box-shadow: 0 10px 25px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 420px; }
		.btn { cursor: pointer; border: none; border-radius: 12px; font-weight: 700; padding: 14px; background: #10b981; color: white; width: 100%; display: block; text-decoration: none; margin-top: 10px; font-size: 16px; border: none; }
		.btn-delete { background: #fee2e2; color: #ef4444; padding: 8px 12px; border-radius: 8px; font-size: 12px; text-decoration: none; font-weight: bold; float: right; border: none; }
		.form-input { font-size: 16px; width: 100%; padding: 12px; border: 2px solid #e2e8f0; border-radius: 12px; margin: 10px 0; outline: none; box-sizing: border-box; text-align: center; }
		.appointment-card { border-left: 4px solid #10b981; background: #f9fafb; padding: 15px; margin-bottom: 12px; border-radius: 8px; text-align: left; position: relative; }
		hr { border: 0; border-top: 1px solid #e2e8f0; margin: 25px 0; }
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
	msg := fmt.Sprintf("Subject: Code %s\r\n\r\nYour code: %s", code, code)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(msg))
}

func main() {
	initDB()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		session, err := r.Cookie("session_valid")
		if err == nil && session.Value == "true" {
			http.Redirect(w, r, "/dashboard", 302)
			return
		}
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

		http.SetCookie(w, &http.Cookie{Name: "user_email", Value: userInfo.Email, Path: "/", MaxAge: 86400, HttpOnly: true})
		http.Redirect(w, r, "/otp-verify", 302)
	})

	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("user_email")
		if err != nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		email := cookie.Value
		// Очищаем старые секреты
		db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", email)

		key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
		secret := key.Secret()
		db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'User', '2026-01-01', 'System')", email, secret)

		// Генерируем код и отправляем
		otpCode, _ := totp.GenerateCode(secret, time.Now())
		go sendEmailOTP(email, otpCode)

		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Введите код</h2><form action='/otp-check' method='POST'><input type='text' name='code' class='form-input' style='letter-spacing:5px;' required autofocus><button type='submit' class='btn'>Подтвердить</button></form></div></body></html>", sharedStyles)
	})

	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' ORDER BY id DESC LIMIT 1", cookie.Value).Scan(&secret)

		// Увеличиваем Skew до 5 (это дает окно в 5 минут, чтобы код точно сработал)
		valid, _ := totp.ValidateCustom(strings.TrimSpace(r.FormValue("code")), strings.TrimSpace(secret), time.Now(), totp.ValidateOpts{
			Skew:   5,
			Digits: 6,
			Period: 30,
		})

		if valid {
			http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400, HttpOnly: true})
			http.Redirect(w, r, "/dashboard", 302)
		} else {
			fmt.Fprintf(w, "<html><body style='text-align:center; font-family:sans-serif;'><h2>Неверный код или время истекло</h2><a href='/otp-verify'>Попробовать еще раз</a></body></html>")
		}
	})

	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		emailCookie, _ := r.Cookie("user_email")
		sessionCookie, err := r.Cookie("session_valid")
		if err != nil || sessionCookie.Value != "true" {
			http.Redirect(w, r, "/", 302)
			return
		}

		rows, _ := db.Query("SELECT id, doctor_name, appointment_date FROM appointments WHERE user_email = $1 AND doctor_name != 'System' ORDER BY id DESC", emailCookie.Value)
		var listHTML string
		for rows.Next() {
			var id int
			var d, dt string
			rows.Scan(&id, &d, &dt)
			listHTML += fmt.Sprintf("<div class='appointment-card'><a href='/delete?id=%d' class='btn-delete'>✕</a><strong>%s</strong><br><small>%s</small></div>", id, d, dt)
		}

		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Кабинет</h2>%s<hr><form action='/add' method='POST'><input name='doc' placeholder='Врач' class='form-input' required><input type='datetime-local' name='date' class='form-input' required><button class='btn'>Записаться</button></form><br><a href='/logout' style='color:#64748b; text-decoration:none;'>Выйти</a></div></body></html>", sharedStyles, listHTML)
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		// Очищаем куки полностью
		http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "", Path: "/", MaxAge: -1})
		http.SetCookie(w, &http.Cookie{Name: "user_email", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/", 302)
	})

	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		if r.Method == "POST" && cookie != nil {
			db.Exec("INSERT INTO appointments (user_email, doctor_name, appointment_date, patient_name, totp_secret) VALUES ($1, $2, $3, 'Пациент', '')",
				cookie.Value, r.FormValue("doc"), r.FormValue("date"))
		}
		http.Redirect(w, r, "/dashboard", 302)
	})

	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		id := r.URL.Query().Get("id")
		if cookie != nil && id != "" {
			db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, cookie.Value)
		}
		http.Redirect(w, r, "/dashboard", 302)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
