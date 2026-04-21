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
		@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;500;600;700&display=swap');
		:root { --primary: #10b981; --primary-dark: #059669; --bg: #f1f5f9; --card-bg: #ffffff; --text-main: #1e293b; --text-muted: #64748b; }
		body { font-family: 'Plus Jakarta Sans', sans-serif; background: var(--bg); color: var(--text-main); display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; padding: 20px; }
		.card { background: var(--card-bg); padding: 40px; border-radius: 32px; box-shadow: 0 20px 50px rgba(0,0,0,0.05); text-align: center; width: 100%; max-width: 480px; animation: slideUp 0.5s ease-out; }
		@keyframes slideUp { from { opacity: 0; transform: translateY(20px); } to { opacity: 1; transform: translateY(0); } }
		.btn { cursor: pointer; border: none; border-radius: 16px; font-weight: 700; padding: 16px; background: linear-gradient(135deg, var(--primary), var(--primary-dark)); color: white; width: 100%; display: block; text-decoration: none; margin-top: 15px; transition: all 0.3s ease; box-shadow: 0 4px 12px rgba(16, 185, 129, 0.2); }
		.btn:hover { transform: translateY(-2px); box-shadow: 0 6px 20px rgba(16, 185, 129, 0.3); }
		.form-input { font-size: 16px; width: 100%; padding: 14px; border: 2px solid #e2e8f0; border-radius: 16px; margin: 8px 0 16px 0; outline: none; box-sizing: border-box; }
		.appointment-card { background: #f8fafc; padding: 18px; margin-bottom: 12px; border-radius: 20px; text-align: left; position: relative; border: 1px solid #f1f5f9; }
		.btn-delete { position: absolute; right: 15px; top: 15px; color: #94a3b8; text-decoration: none; font-size: 18px; }
		label { display: block; text-align: left; font-size: 13px; font-weight: 600; color: var(--text-muted); margin-left: 5px; }
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

// Отправка кода OTP
func sendEmailOTP(toEmail, code string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}
	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	msg := fmt.Sprintf("Subject: %s - Код доступа HealthTech\r\n\r\nВаш проверочный код: %s", code, code)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, []byte(msg))
}

// НОВАЯ ФУНКЦИЯ: Отправка уведомления о записи
func sendAppointmentEmail(toEmail, doctor, dateStr string) {
	from, pass := os.Getenv("EMAIL_USER"), os.Getenv("EMAIL_PASS")
	if from == "" || pass == "" {
		return
	}

	t, _ := time.Parse("2006-01-02T15:04", dateStr)
	formattedDate := t.Format("02.01.2006 в 15:04")

	auth := smtp.PlainAuth("", from, pass, "smtp.gmail.com")
	subject := "Subject: Подтверждение записи - HealthTech\r\n"
	mime := "MIME-version: 1.0; Content-Type: text/html; charset=\"UTF-8\";\r\n\r\n"
	body := fmt.Sprintf(`
		<div style="font-family: sans-serif; padding: 20px; border: 1px solid #e2e8f0; border-radius: 10px;">
			<h2 style="color: #10b981;">🌿 Запись подтверждена!</h2>
			<p>Здравствуйте!</p>
			<p>Вы успешно записались на прием:</p>
			<div style="background: #f8fafc; padding: 15px; border-radius: 8px;">
				<strong>Врач:</strong> %s<br>
				<strong>Дата и время:</strong> %s
			</div>
			<p style="color: #64748b; font-size: 12px; margin-top: 20px;">Если планы изменились, отмените запись в личном кабинете.</p>
		</div>
	`, doctor, formattedDate)

	msg := []byte(subject + mime + body)
	_ = smtp.SendMail("smtp.gmail.com:587", auth, from, []string{toEmail}, msg)
}

func main() {
	initDB()

	// Главная
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		session, err := r.Cookie("session_valid")
		if err == nil && session.Value == "true" {
			http.Redirect(w, r, "/dashboard", 302)
			return
		}
		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'><meta name='viewport' content='width=device-width, initial-scale=1.0'>%s</head><body><div class='card'><h1>🌿 HealthTech</h1><a href='%s' class='btn'>Войти через Google</a></div></body></html>", sharedStyles, googleOAuthConfig.AuthCodeURL("state"))
	})

	// Callback OAuth
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

	// OTP
	http.HandleFunc("/otp-verify", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		email := cookie.Value
		db.Exec("DELETE FROM appointments WHERE user_email = $1 AND doctor_name = 'System'", email)
		key, _ := totp.Generate(totp.GenerateOpts{Issuer: "HealthTech", AccountName: email})
		secret := key.Secret()
		db.Exec("INSERT INTO appointments (user_email, totp_secret, patient_name, appointment_date, doctor_name) VALUES ($1, $2, 'User', '2026-01-01', 'System')", email, secret)
		otp, _ := totp.GenerateCode(secret, time.Now())
		go sendEmailOTP(email, otp)
		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'>%s</head><body><div class='card'><h2>Код из почты</h2><form action='/otp-check' method='POST'><input type='text' name='code' class='form-input' style='text-align:center;' required autofocus><button type='submit' class='btn'>Войти</button></form></div></body></html>", sharedStyles)
	})

	http.HandleFunc("/otp-check", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		var secret string
		db.QueryRow("SELECT totp_secret FROM appointments WHERE user_email = $1 AND doctor_name = 'System' ORDER BY id DESC LIMIT 1", cookie.Value).Scan(&secret)
		valid, _ := totp.ValidateCustom(strings.TrimSpace(r.FormValue("code")), strings.TrimSpace(secret), time.Now(), totp.ValidateOpts{Skew: 5})
		if valid {
			http.SetCookie(w, &http.Cookie{Name: "session_valid", Value: "true", Path: "/", MaxAge: 86400, HttpOnly: true})
			http.Redirect(w, r, "/dashboard", 302)
		} else {
			fmt.Fprintf(w, "<script>alert('Ошибка'); window.history.back();</script>")
		}
	})

	// Dashboard
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		emailCookie, _ := r.Cookie("user_email")
		sessionCookie, _ := r.Cookie("session_valid")
		if sessionCookie == nil {
			http.Redirect(w, r, "/", 302)
			return
		}

		rows, _ := db.Query("SELECT id, doctor_name, appointment_date FROM appointments WHERE user_email = $1 AND doctor_name != 'System' ORDER BY appointment_date ASC", emailCookie.Value)
		var listHTML string
		for rows.Next() {
			var id int
			var d, dt string
			rows.Scan(&id, &d, &dt)
			t, _ := time.Parse("2006-01-02T15:04", dt)
			listHTML += fmt.Sprintf("<div class='appointment-card' data-doctor='%s'><a href='/delete?id=%d' class='btn-delete'>✕</a><strong>%s</strong><br><small>%s</small></div>", d, id, d, t.Format("02.01.2006 15:04"))
		}

		fmt.Fprintf(w, "<html><head><meta charset='UTF-8'><meta name='viewport' content='width=device-width, initial-scale=1.0'>%s</head><body><div class='card'><h2>Мои записи</h2><div id='list'>%s</div><hr><form action='/add' method='POST'><label>Врач</label><input name='doc' class='form-input' required><label>Когда</label><input type='datetime-local' name='date' class='form-input' required><button class='btn'>Записаться</button></form><br><a href='/logout' style='color:#94a3b8; text-decoration:none;'>Выход</a></div></body></html>", sharedStyles, listHTML)
	})

	// ОБНОВЛЕННЫЙ ADD: Добавляем отправку уведомления
	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		cookie, _ := r.Cookie("user_email")
		doc := strings.TrimSpace(r.FormValue("doc"))
		date := r.FormValue("date")

		if cookie != nil && doc != "" {
			// 1. Сохраняем в базу
			_, err := db.Exec("INSERT INTO appointments (user_email, doctor_name, appointment_date, patient_name, totp_secret) VALUES ($1, $2, $3, 'Patient', '')", cookie.Value, doc, date)

			// 2. Если сохранилось, отправляем письмо
			if err == nil {
				go sendAppointmentEmail(cookie.Value, doc, date)
			}
		}
		http.Redirect(w, r, "/dashboard", 302)
	})

	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		email, _ := r.Cookie("user_email")
		if id != "" && email != nil {
			db.Exec("DELETE FROM appointments WHERE id = $1 AND user_email = $2", id, email.Value)
		}
		http.Redirect(w, r, "/dashboard", 302)
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
